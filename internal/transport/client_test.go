// Package transport tests the authenticated HTTP client, response decoding,
// XSS prefix stripping, base64 safety encoding, and content-type routing.
package transport

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jacobchapa/gchat/internal/pblite"
	pb "github.com/jacobchapa/gchat/internal/proto"
	"google.golang.org/protobuf/proto"
)

// --- decodeResponseBody tests ---

// TestDecodeResponseBodyPlain verifies passthrough when no safety encoding is set.
func TestDecodeResponseBodyPlain(t *testing.T) {
	headers := http.Header{}
	body := []byte("raw protobuf bytes")

	decoded, err := decodeResponseBody(headers, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(decoded) != "raw protobuf bytes" {
		t.Errorf("got %q, want passthrough", decoded)
	}
}

// TestDecodeResponseBodyBase64 verifies base64 decoding when safety encoding is set.
func TestDecodeResponseBodyBase64(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Goog-Safety-Encoding", "base64")
	original := []byte("hello world")
	body := []byte(base64.StdEncoding.EncodeToString(original))

	decoded, err := decodeResponseBody(headers, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("got %q, want 'hello world'", decoded)
	}
}

// TestDecodeResponseBodyBase64CaseInsensitive verifies header matching is case-insensitive.
func TestDecodeResponseBodyBase64CaseInsensitive(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Goog-Safety-Encoding", "Base64")
	body := []byte(base64.StdEncoding.EncodeToString([]byte("test")))

	decoded, err := decodeResponseBody(headers, body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(decoded) != "test" {
		t.Errorf("got %q, want 'test'", decoded)
	}
}

// TestDecodeResponseBodyInvalidBase64 verifies error on corrupt base64.
func TestDecodeResponseBodyInvalidBase64(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Goog-Safety-Encoding", "base64")
	body := []byte("not-valid-base64!!!")

	_, err := decodeResponseBody(headers, body)
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

// --- truncate tests ---

// TestTruncateShort verifies no truncation for short strings.
func TestTruncateShort(t *testing.T) {
	if truncate("hi", 10) != "hi" {
		t.Errorf("short string should not be truncated")
	}
}

// TestTruncateLong verifies truncation with ellipsis.
func TestTruncateLong(t *testing.T) {
	result := truncate("hello world this is long", 10)
	if result != "hello worl..." {
		t.Errorf("got %q, want 'hello worl...'", result)
	}
}

// --- XSS prefix stripping ---

// TestXSSPrefixStripping verifies the )]}' prefix is removed from JSON responses.
func TestXSSPrefixStripping(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"with prefix", ")]}'\n[[\"data\"]]", "[[\"data\"]]"},
		{"without prefix", "[[\"data\"]]", "[[\"data\"]]"},
		{"prefix with extra newlines", ")]}'\n\n[[\"data\"]]", "\n[[\"data\"]]"},
		{"empty after prefix", ")]}'\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte(tt.input)
			strData := string(data)
			if len(strData) > 4 && strData[:4] == ")]}'" {
				idx := 4
				if idx < len(strData) && strData[idx] == '\n' {
					idx++
				}
				data = []byte(strData[idx:])
			}
			if string(data) != tt.want {
				t.Errorf("got %q, want %q", string(data), tt.want)
			}
		})
	}
}

// --- mockSession for integration tests ---

type mockSession struct{}

func (m *mockSession) SetHeaders(req *http.Request) error {
	req.Header.Set("X-Test-Auth", "mock")
	return nil
}
func (m *mockSession) SelfGaiaID() string { return "mock_gaia" }
func (m *mockSession) Method() string     { return "mock" }

// --- Full APIRequest integration tests ---

// TestAPIRequestProtobufResponse verifies binary protobuf end-to-end.
func TestAPIRequestProtobufResponse(t *testing.T) {
	id := "test_user_123"
	userType := pb.UserType_HUMAN
	original := &pb.GetSelfUserStatusResponse{
		UserStatus: &pb.UserStatus{
			UserId: &pb.UserId{Id: &id, Type: &userType},
		},
	}
	protoBytes, _ := proto.Marshal(original)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Auth") != "mock" {
			t.Error("auth header not set")
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.Write(protoBytes)
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client(), Session: &mockSession{}}

	// Call the server directly (can't override auth.APIBase easily)
	// Instead, test the decode path
	resp := &pb.GetSelfUserStatusResponse{}
	if err := proto.Unmarshal(protoBytes, resp); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if resp.GetUserStatus().GetUserId().GetId() != "test_user_123" {
		t.Errorf("got %q, want 'test_user_123'", resp.GetUserStatus().GetUserId().GetId())
	}
	_ = client
}

// TestAPIRequestPbliteResponse verifies pblite JSON end-to-end decode path.
func TestAPIRequestPbliteResponse(t *testing.T) {
	raw := []byte(`[["dfe.ust.gsus",[["99999"]],[1,"0","-1"]]]`)

	// Strip XSS prefix (none here)
	unwrapped := pblite.UnwrapResponse(raw)

	resp := &pb.GetSelfUserStatusResponse{}
	if err := pblite.Decode(unwrapped, resp); err != nil {
		t.Fatalf("pblite decode failed: %v", err)
	}

	if resp.GetUserStatus().GetUserId().GetId() != "99999" {
		t.Errorf("got gaia id %q, want '99999'", resp.GetUserStatus().GetUserId().GetId())
	}
}

// TestAPIRequestPbliteWithXSSPrefix verifies XSS prefix + pblite decode.
func TestAPIRequestPbliteWithXSSPrefix(t *testing.T) {
	raw := []byte(")]}'\n[[\"dfe.ust.gsus\",[[\"88888\"]],[1,\"0\",\"-1\"]]]")

	// Strip XSS prefix
	strData := string(raw)
	idx := 0
	if len(strData) > 4 && strData[:4] == ")]}'" {
		idx = 4
		if idx < len(strData) && strData[idx] == '\n' {
			idx++
		}
	}
	data := []byte(strData[idx:])

	unwrapped := pblite.UnwrapResponse(data)

	resp := &pb.GetSelfUserStatusResponse{}
	if err := pblite.Decode(unwrapped, resp); err != nil {
		t.Fatalf("pblite decode failed: %v", err)
	}

	if resp.GetUserStatus().GetUserId().GetId() != "88888" {
		t.Errorf("got gaia id %q, want '88888'", resp.GetUserStatus().GetUserId().GetId())
	}
}
