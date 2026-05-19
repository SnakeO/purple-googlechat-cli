// cookie.go implements cookie-based authentication for Google Chat.
// Users extract cookies from a logged-in browser session at chat.google.com,
// then this module bootstraps an XSRF token from the mole/world endpoint.
package auth

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RequiredCookies lists the cookie names needed for authentication.
var RequiredCookies = []string{"COMPASS", "SSID", "SID", "OSID", "HSID"}

// CookieSession holds state for cookie-based authentication.
type CookieSession struct {
	cookies map[string]string
	xsrf    string
	sapisid string
	gaiaID  string
}

// NewCookieSession creates a session from browser cookies.
// Validates that all required cookies are present.
func NewCookieSession(cookies map[string]string) (*CookieSession, error) {
	for _, name := range RequiredCookies {
		if cookies[name] == "" {
			return nil, fmt.Errorf("auth: missing required cookie: %s", name)
		}
	}

	return &CookieSession{
		cookies: cookies,
		sapisid: cookies["SAPISID"],
	}, nil
}

// SetHeaders adds cookie auth headers to an HTTP request.
func (s *CookieSession) SetHeaders(req *http.Request) error {
	req.Header.Set("User-Agent", UserAgent)

	// Set cookies
	for name, val := range s.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}

	// Set XSRF token if available
	if s.xsrf != "" {
		req.Header.Set("X-Framework-XSRF-Token", s.xsrf)
	}

	return nil
}

// SelfGaiaID returns the authenticated user's Google Account ID.
func (s *CookieSession) SelfGaiaID() string {
	return s.gaiaID
}

// Method returns "cookie".
func (s *CookieSession) Method() string {
	return "cookie"
}

// SetGaiaID stores the user's Gaia ID after it's fetched from the API.
func (s *CookieSession) SetGaiaID(id string) {
	s.gaiaID = id
}

// XSRF returns the current XSRF token.
func (s *CookieSession) XSRF() string {
	return s.xsrf
}

// SetXSRF sets the XSRF token (used when restoring from saved credentials).
func (s *CookieSession) SetXSRF(token string) {
	s.xsrf = token
}

// BootstrapXSRF fetches the XSRF token from the mole/world endpoint.
// This must be called before making API requests with cookie auth.
func (s *CookieSession) BootstrapXSRF(client *http.Client) error {
	moleURL := buildMoleWorldURL()

	req, err := http.NewRequest("GET", moleURL, nil)
	if err != nil {
		return fmt.Errorf("auth: cannot create mole/world request: %w", err)
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", "https://mail.google.com/")
	for name, val := range s.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("auth: mole/world request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("auth: mole/world returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("auth: cannot read mole/world response: %w", err)
	}

	xsrf, sapisid, err := parseWizGlobalData(string(body))
	if err != nil {
		return err
	}

	s.xsrf = xsrf
	if sapisid != "" {
		s.sapisid = sapisid
	}

	return nil
}

// buildMoleWorldURL constructs the mole/world URL with required query params.
func buildMoleWorldURL() string {
	params := url.Values{
		"origin": {"https://mail.google.com"},
		"shell":  {"9"},
		"hl":     {"en"},
		"wfi":    {"gtn-roster-iframe-id"},
		"hs":     {`["h_hs",null,null,[1,0],null,null,"gmail.pinto-server_20230730.06_p0",1,null,[15,38,36,35,26,30,41,18,24,11,21,14,6],null,null,"3Mu86PSulM4.en..es5",0,null,null,[0]]`},
	}
	return MoleWorldURL + "?" + params.Encode()
}

// parseWizGlobalData extracts the XSRF token and SAPISID from the mole/world HTML.
// Looks for window.WIZ_global_data JSON and pulls the SMqcke and WZsZ1e fields.
func parseWizGlobalData(html string) (xsrf string, sapisid string, err error) {
	marker := ">window.WIZ_global_data = "
	start := strings.Index(html, marker)
	if start == -1 {
		return "", "", fmt.Errorf("auth: WIZ_global_data not found in mole/world response")
	}
	start += len(marker)

	end := strings.Index(html[start:], ";</script>")
	if end == -1 {
		return "", "", fmt.Errorf("auth: WIZ_global_data end marker not found")
	}

	jsonStr := html[start : start+end]

	// Extract XSRF token (SMqcke field)
	xsrf = extractJSONStringField(jsonStr, "SMqcke")
	if xsrf == "" {
		return "", "", fmt.Errorf("auth: XSRF token (SMqcke) not found in WIZ_global_data")
	}

	// Extract SAPISID (WZsZ1e field) — optional
	sapisid = extractJSONStringField(jsonStr, "WZsZ1e")

	return xsrf, sapisid, nil
}

// extractJSONStringField does a simple key-value extraction from a JSON string.
// Avoids pulling in a full JSON parser for this small task.
func extractJSONStringField(json string, key string) string {
	needle := `"` + key + `":"`
	idx := strings.Index(json, needle)
	if idx == -1 {
		return ""
	}

	start := idx + len(needle)
	end := strings.Index(json[start:], `"`)
	if end == -1 {
		return ""
	}

	return json[start : start+end]
}
