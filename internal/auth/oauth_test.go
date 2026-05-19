// Package auth tests the OAuth flow: code exchange, token refresh,
// Dynamite token fetch, and the local callback server.
package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAuthorizationURL verifies the OAuth URL contains required parameters.
func TestAuthorizationURL(t *testing.T) {
	url := AuthorizationURL()

	required := []string{
		"client_id=",
		"redirect_uri=",
		"response_type=code",
		"scope=",
		"access_type=offline",
	}

	for _, param := range required {
		if !containsStr(url, param) {
			t.Errorf("URL missing %q: %s", param, url)
		}
	}
}

// TestExchangeCodeSuccess verifies authorization code → token exchange.
func TestExchangeCodeSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		r.ParseForm()
		if r.Form.Get("grant_type") != "authorization_code" {
			t.Errorf("wrong grant_type: %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("code") != "test_auth_code" {
			t.Errorf("wrong code: %s", r.Form.Get("code"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "mock_access_token",
			"id_token":      "mock_id_token",
			"refresh_token": "mock_refresh_token",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	// Save and restore the token URL
	origURL := OAuthTokenURL
	OAuthTokenURL = server.URL
	defer func() { OAuthTokenURL = origURL }()

	accessToken, refreshToken, err := exchangeCode("test_auth_code", server.Client())
	if err != nil {
		t.Fatalf("exchangeCode failed: %v", err)
	}

	// id_token is preferred over access_token
	if accessToken != "mock_id_token" {
		t.Errorf("accessToken: got %q, want 'mock_id_token'", accessToken)
	}
	if refreshToken != "mock_refresh_token" {
		t.Errorf("refreshToken: got %q, want 'mock_refresh_token'", refreshToken)
	}
}

// TestExchangeCodeFallsBackToAccessToken verifies access_token is used when id_token is absent.
func TestExchangeCodeFallsBackToAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "only_access_token",
			"refresh_token": "refresh",
			"expires_in":    3600,
		})
	}))
	defer server.Close()

	origURL := OAuthTokenURL
	OAuthTokenURL = server.URL
	defer func() { OAuthTokenURL = origURL }()

	accessToken, _, err := exchangeCode("code", server.Client())
	if err != nil {
		t.Fatalf("exchangeCode failed: %v", err)
	}

	if accessToken != "only_access_token" {
		t.Errorf("accessToken: got %q, want 'only_access_token'", accessToken)
	}
}

// TestExchangeCodeHTTPError verifies error handling for non-200 responses.
func TestExchangeCodeHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid_grant"}`))
	}))
	defer server.Close()

	origURL := OAuthTokenURL
	OAuthTokenURL = server.URL
	defer func() { OAuthTokenURL = origURL }()

	_, _, err := exchangeCode("bad_code", server.Client())
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
}

// TestRefreshIDToken verifies the refresh token → id_token flow.
func TestRefreshIDToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("wrong grant_type: %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "stored_refresh" {
			t.Errorf("wrong refresh_token: %s", r.Form.Get("refresh_token"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "refreshed_id_token",
			"expires_in":   3600,
		})
	}))
	defer server.Close()

	origURL := OAuthTokenURL
	OAuthTokenURL = server.URL
	defer func() { OAuthTokenURL = origURL }()

	session := &OAuthSession{
		refreshToken: "stored_refresh",
		client:       server.Client(),
	}

	if err := session.refreshIDToken(); err != nil {
		t.Fatalf("refreshIDToken failed: %v", err)
	}
	if session.idToken != "refreshed_id_token" {
		t.Errorf("idToken: got %q, want 'refreshed_id_token'", session.idToken)
	}
}

// TestFetchDynamiteToken verifies the id_token → Dynamite access_token exchange.
func TestFetchDynamiteToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my_id_token" {
			t.Errorf("wrong Authorization: %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":     "dynamite_access_token",
			"expiresIn": "3600",
		})
	}))
	defer server.Close()

	origURL := DynamiteIssueTokenURL
	DynamiteIssueTokenURL = server.URL
	defer func() { DynamiteIssueTokenURL = origURL }()

	session := &OAuthSession{
		idToken: "my_id_token",
		client:  server.Client(),
	}

	if err := session.fetchDynamiteToken(); err != nil {
		t.Fatalf("fetchDynamiteToken failed: %v", err)
	}
	if session.accessToken != "dynamite_access_token" {
		t.Errorf("accessToken: got %q, want 'dynamite_access_token'", session.accessToken)
	}
}

// TestOAuthSessionSetHeaders verifies Bearer token injection.
func TestOAuthSessionSetHeaders(t *testing.T) {
	session := &OAuthSession{
		accessToken: "test_bearer",
		expiresAt:   time.Now().Add(1 * time.Hour),
	}

	req, _ := http.NewRequest("GET", "https://example.com", nil)
	if err := session.SetHeaders(req); err != nil {
		t.Fatalf("SetHeaders failed: %v", err)
	}

	if req.Header.Get("Authorization") != "Bearer test_bearer" {
		t.Errorf("Authorization: got %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("User-Agent") != UserAgent {
		t.Error("User-Agent not set")
	}
}

// TestSetOAuthCredentials verifies custom client ID override.
func TestSetOAuthCredentials(t *testing.T) {
	origID := OAuthClientID
	origSecret := OAuthClientSecret
	defer func() {
		OAuthClientID = origID
		OAuthClientSecret = origSecret
	}()

	SetOAuthCredentials("custom_id", "custom_secret")
	if OAuthClientID != "custom_id" {
		t.Errorf("got %q, want 'custom_id'", OAuthClientID)
	}
	if OAuthClientSecret != "custom_secret" {
		t.Errorf("got %q, want 'custom_secret'", OAuthClientSecret)
	}
}

// containsStr checks if a string contains a substring.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStrHelper(s, substr))
}

func containsStrHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
