// Package auth provides authentication for Google Chat's internal Dynamite protocol.
// Supports two methods: cookie-based (from browser DevTools) and OAuth 2.0.
// Both methods produce a Session that can inject auth headers into HTTP requests.
package auth

import (
	"net/http"
)

// Session represents an authenticated Google Chat session.
// Both cookie and OAuth implementations satisfy this interface.
type Session interface {
	// SetHeaders adds authentication headers to an outgoing HTTP request.
	SetHeaders(req *http.Request) error

	// SelfGaiaID returns the authenticated user's Google Account ID.
	SelfGaiaID() string

	// Method returns "cookie" or "oauth".
	Method() string
}

// Protocol constants for Google Chat's Dynamite API.
const (
	APIBase        = "https://chat.google.com"
	WebchannelBase = "https://chat.google.com/webchannel/"
	MoleWorldURL   = "https://chat.google.com/mole/world"

	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36"

	DefaultOAuthClientID     = "936475272427.apps.googleusercontent.com"
	DefaultOAuthClientSecret = "KWsJlkaMn1jGLxQpWxMnOox-"
	OAuthRedirectURI = "http://localhost:8855/callback"
	OAuthAuthURL     = "https://accounts.google.com/o/oauth2/auth"
	OAuthScope       = "openid email profile"
	DynamiteClientID      = "576267593750-sbi1m7khesgfh1e0f2nv5vqlfa4qr72m.apps.googleusercontent.com"
	DynamiteAppID         = "com.google.Dynamite"
	DynamiteScopes        = "https://www.googleapis.com/auth/dynamite https://www.googleapis.com/auth/drive https://www.googleapis.com/auth/mobiledevicemanagement https://www.googleapis.com/auth/notifications https://www.googleapis.com/auth/supportcontent https://www.googleapis.com/auth/chat.integration https://www.googleapis.com/auth/peopleapi.readonly"
)

// Mutable auth settings — can be overridden for custom clients or testing.
var (
	OAuthClientID         = DefaultOAuthClientID
	OAuthClientSecret     = DefaultOAuthClientSecret
	OAuthTokenURL         = "https://www.googleapis.com/oauth2/v4/token"
	DynamiteIssueTokenURL = "https://oauthaccountmanager.googleapis.com/v1/issuetoken"
)

// SetOAuthCredentials overrides the default OAuth client credentials.
func SetOAuthCredentials(clientID, clientSecret string) {
	OAuthClientID = clientID
	OAuthClientSecret = clientSecret
}
