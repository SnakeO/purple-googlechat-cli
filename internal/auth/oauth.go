// oauth.go implements OAuth 2.0 authentication for Google Chat.
// Flow: authorization code → id_token + refresh_token → Dynamite access_token.
// The Dynamite token is used as a Bearer token for all API requests.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthSession holds state for OAuth-based authentication.
type OAuthSession struct {
	refreshToken string
	idToken      string
	accessToken  string
	gaiaID       string
	expiresAt    time.Time
	client       *http.Client
}

// AuthorizationURL returns the URL the user should open to authorize the app.
func AuthorizationURL() string {
	params := url.Values{
		"client_id":     {OAuthClientID},
		"scope":         {OAuthScope},
		"redirect_uri":  {OAuthRedirectURI},
		"response_type": {"code"},
		"access_type":   {"offline"},
	}
	return OAuthAuthURL + "?" + params.Encode()
}

// WaitForAuthCode starts a local HTTP server and waits for the OAuth callback.
// Returns the authorization code from the callback query parameter.
func WaitForAuthCode() (string, error) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no code in callback"
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "<h1>Authentication failed</h1><p>%s</p>", errMsg)
			errCh <- fmt.Errorf("auth callback error: %s", errMsg)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<h1>Authenticated!</h1><p>You can close this window and return to the terminal.</p>")
		codeCh <- code
	})

	server := &http.Server{
		Handler: mux,
	}

	listener, err := net.Listen("tcp", "localhost:8855")
	if err != nil {
		return "", fmt.Errorf("auth: cannot start local server on :8855: %w", err)
	}

	go server.Serve(listener)
	defer server.Shutdown(context.Background())

	select {
	case code := <-codeCh:
		return code, nil
	case err := <-errCh:
		return "", err
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("auth: timed out waiting for callback (5 minutes)")
	}
}

// NewOAuthSessionFromCode exchanges an authorization code for tokens.
func NewOAuthSessionFromCode(code string, client *http.Client) (*OAuthSession, error) {
	idToken, refreshToken, err := exchangeCode(code, client)
	if err != nil {
		return nil, err
	}

	session := &OAuthSession{
		refreshToken: refreshToken,
		idToken:      idToken,
		client:       client,
	}

	if err := session.fetchDynamiteToken(); err != nil {
		return nil, fmt.Errorf("auth: dynamite token exchange failed: %w", err)
	}

	return session, nil
}

// NewOAuthSessionFromRefreshToken restores a session from a stored refresh token.
func NewOAuthSessionFromRefreshToken(refreshToken string, client *http.Client) (*OAuthSession, error) {
	session := &OAuthSession{
		refreshToken: refreshToken,
		client:       client,
	}

	if err := session.refreshIDToken(); err != nil {
		return nil, fmt.Errorf("auth: token refresh failed: %w", err)
	}

	if err := session.fetchDynamiteToken(); err != nil {
		return nil, fmt.Errorf("auth: dynamite token exchange failed: %w", err)
	}

	return session, nil
}

// SetHeaders adds OAuth Bearer token to an HTTP request.
func (s *OAuthSession) SetHeaders(req *http.Request) error {
	if time.Now().After(s.expiresAt) {
		if err := s.Refresh(); err != nil {
			return err
		}
	}

	req.Header.Set("Authorization", "Bearer "+s.accessToken)
	req.Header.Set("User-Agent", UserAgent)
	return nil
}

// SelfGaiaID returns the authenticated user's Google Account ID.
func (s *OAuthSession) SelfGaiaID() string {
	return s.gaiaID
}

// Method returns "oauth".
func (s *OAuthSession) Method() string {
	return "oauth"
}

// SetGaiaID stores the user's Gaia ID after it's fetched from the API.
func (s *OAuthSession) SetGaiaID(id string) {
	s.gaiaID = id
}

// RefreshToken returns the stored refresh token for persistence.
func (s *OAuthSession) RefreshToken() string {
	return s.refreshToken
}

// AccessToken returns the current Dynamite access token.
func (s *OAuthSession) AccessToken() string {
	return s.accessToken
}

// Refresh renews the id_token and Dynamite access_token.
func (s *OAuthSession) Refresh() error {
	if err := s.refreshIDToken(); err != nil {
		return err
	}
	return s.fetchDynamiteToken()
}

// exchangeCode trades an authorization code for tokens.
// Returns the access_token (used as Bearer for Dynamite exchange) and refresh_token.
func exchangeCode(code string, client *http.Client) (accessToken, refreshToken string, err error) {
	data := url.Values{
		"client_id":     {OAuthClientID},
		"client_secret": {OAuthClientSecret},
		"code":          {code},
		"redirect_uri":  {OAuthRedirectURI},
		"grant_type":    {"authorization_code"},
	}

	resp, err := client.PostForm(OAuthTokenURL, data)
	if err != nil {
		return "", "", fmt.Errorf("auth: token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("auth: cannot read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("auth: token exchange failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("auth: cannot parse token response: %w", err)
	}

	// Prefer access_token for Dynamite exchange (works with standard scopes)
	token := result.AccessToken
	if result.IDToken != "" {
		token = result.IDToken
	}

	return token, result.RefreshToken, nil
}

// refreshIDToken uses the refresh token to get a new id_token (access_token in response).
func (s *OAuthSession) refreshIDToken() error {
	data := url.Values{
		"client_id":     {OAuthClientID},
		"refresh_token": {s.refreshToken},
		"grant_type":    {"refresh_token"},
	}

	resp, err := s.client.PostForm(OAuthTokenURL, data)
	if err != nil {
		return fmt.Errorf("auth: refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("auth: cannot read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: refresh failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("auth: cannot parse refresh response: %w", err)
	}

	s.idToken = result.AccessToken
	return nil
}

// fetchDynamiteToken exchanges an id_token for a Dynamite access token.
func (s *OAuthSession) fetchDynamiteToken() error {
	data := url.Values{
		"app_id":          {DynamiteAppID},
		"client_id":       {DynamiteClientID},
		"passcode_present": {"YES"},
		"response_type":   {"token"},
		"scope":           {DynamiteScopes},
	}

	req, err := http.NewRequest("POST", DynamiteIssueTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("auth: cannot create dynamite request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.idToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: dynamite request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("auth: cannot read dynamite response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: dynamite token failed (status %d): %s", resp.StatusCode, body)
	}

	var result struct {
		Token     string `json:"token"`
		ExpiresIn string `json:"expiresIn"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("auth: cannot parse dynamite response: %w", err)
	}

	s.accessToken = result.Token

	// Parse expiry, default to 1 hour
	expiresIn := 3600
	if result.ExpiresIn != "" {
		fmt.Sscanf(result.ExpiresIn, "%d", &expiresIn)
	}
	s.expiresAt = time.Now().Add(time.Duration(expiresIn-30) * time.Second)

	return nil
}
