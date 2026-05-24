// Package transport provides the authenticated HTTP client for Google Chat API requests.
// It wraps an auth.Session to inject headers and handles response content-type detection,
// base64 safety encoding, and protobuf/pblite content negotiation.
package transport

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jacobchapa/gchat/internal/auth"
	"github.com/jacobchapa/gchat/internal/pblite"
	"google.golang.org/protobuf/proto"
)

// Client wraps an HTTP client with Google Chat authentication.
type Client struct {
	HTTP    *http.Client
	Session auth.Session
}

// NewClient creates a new authenticated transport client.
func NewClient(session auth.Session) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 15 * time.Second},
		Session: session,
	}
}

// APIRequestRaw sends a protobuf request and returns the raw response bytes.
func (c *Client) APIRequestRaw(endpoint string, reqMsg proto.Message) ([]byte, string, error) {
	reqBody, err := proto.Marshal(reqMsg)
	if err != nil {
		return nil, "", fmt.Errorf("transport: cannot marshal request: %w", err)
	}

	url := auth.APIBase + endpoint
	if strings.Contains(endpoint, "?") {
		url += "&alt=proto"
	} else {
		url += "?alt=proto"
	}
	req, err := http.NewRequest("POST", url, strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, "", fmt.Errorf("transport: cannot create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-protobuf")
	if err := c.Session.SetHeaders(req); err != nil {
		return nil, "", fmt.Errorf("transport: cannot set auth headers: %w", err)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("transport: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("transport: cannot read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("transport: API returned status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	ct := resp.Header.Get("Content-Type")
	decoded, err := decodeResponseBody(resp.Header, body)
	return decoded, ct, err
}

// APIRequest sends a protobuf request and unmarshals the response.
// Handles both binary protobuf and pblite (JSON array) responses.
func (c *Client) APIRequest(endpoint string, reqMsg proto.Message, respMsg proto.Message) error {
	data, ct, err := c.APIRequestRaw(endpoint, reqMsg)
	if err != nil {
		return err
	}

	// Strip Google's XSS protection prefix from JSON responses
	strData := string(data)
	if strings.HasPrefix(strData, ")]}'") {
		idx := strings.Index(strData, "\n")
		if idx >= 0 {
			data = []byte(strData[idx+1:])
		}
	}

	// Try binary protobuf first
	if strings.Contains(ct, "protobuf") || strings.Contains(ct, "octet-stream") {
		return proto.Unmarshal(data, respMsg)
	}

	// Fall back to pblite (JSON array).
	// Google wraps pblite responses as [["method.name", [actual_data]]].
	// We need to unwrap to get the inner array.
	unwrapped := pblite.UnwrapResponse(data)
	return pblite.Decode(unwrapped, respMsg)
}

// decodeResponseBody handles base64 safety encoding and content-type detection.
func decodeResponseBody(headers http.Header, body []byte) ([]byte, error) {
	encoding := headers.Get("X-Goog-Safety-Encoding")
	if strings.EqualFold(encoding, "base64") {
		decoded, err := base64.StdEncoding.DecodeString(string(body))
		if err != nil {
			return nil, fmt.Errorf("transport: cannot decode base64 response: %w", err)
		}
		return decoded, nil
	}
	return body, nil
}

// DownloadAttachment fetches an attachment by token and returns the file bytes and filename.
// Uses a cookie jar to follow redirects with auth through Google's redirect chain.
func (c *Client) DownloadAttachment(token string) ([]byte, string, error) {
	dlURL := auth.APIBase + "/api/get_attachment_url?url_type=DOWNLOAD_URL&attachment_token=" + url.QueryEscape(token)

	req, err := http.NewRequest("GET", dlURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("transport: cannot create download request: %w", err)
	}

	if err := c.Session.SetHeaders(req); err != nil {
		return nil, "", fmt.Errorf("transport: cannot set auth headers: %w", err)
	}

	// Follow redirects, re-injecting auth headers on each hop
	dlClient := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			c.Session.SetHeaders(r)
			return nil
		},
	}

	resp, err := dlClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("transport: download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("transport: download returned status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("transport: cannot read download response: %w", err)
	}

	filename := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if idx := strings.Index(cd, "filename="); idx >= 0 {
			filename = strings.Trim(cd[idx+9:], "\"' ")
		}
	}

	return body, filename, nil
}

// truncate shortens a string to maxLen characters for error messages.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
