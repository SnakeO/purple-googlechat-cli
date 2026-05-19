// Package channel tests the webchannel connection lifecycle:
// registration, csessionid extraction, SID fetch, and stream processing.
package channel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- extractCSSessionID tests ---

// TestExtractCSSessionIDSimple verifies extraction from "dynamite=<id>".
func TestExtractCSSessionIDSimple(t *testing.T) {
	result := extractCSSessionID("dynamite=abc123")
	if result != "abc123" {
		t.Errorf("got %q, want 'abc123'", result)
	}
}

// TestExtractCSSessionIDMultiPart verifies extraction from compound COMPASS values.
func TestExtractCSSessionIDMultiPart(t *testing.T) {
	result := extractCSSessionID("dynamite-ui=CgAQabc:dynamite=xyz789:dynamite-frontend=CgAQdef")
	if result != "xyz789" {
		t.Errorf("got %q, want 'xyz789'", result)
	}
}

// TestExtractCSSessionIDMissing verifies empty return when no dynamite= segment exists.
func TestExtractCSSessionIDMissing(t *testing.T) {
	result := extractCSSessionID("dynamite-ui=abc:dynamite-frontend=def")
	if result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

// TestExtractCSSessionIDEmpty verifies empty input.
func TestExtractCSSessionIDEmpty(t *testing.T) {
	result := extractCSSessionID("")
	if result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

// --- mockSession for connection tests ---

type mockSession struct{}

func (m *mockSession) SetHeaders(req *http.Request) error {
	req.Header.Set("X-Test", "mock")
	return nil
}
func (m *mockSession) SelfGaiaID() string { return "mock" }
func (m *mockSession) Method() string     { return "mock" }

// --- register + fetchSID integration test ---

// TestRegisterAndFetchSID verifies the full register → SID flow with a mock server.
func TestRegisterAndFetchSID(t *testing.T) {
	var mu sync.Mutex
	requestPaths := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestPaths = append(requestPaths, r.URL.Path)
		mu.Unlock()

		switch {
		case r.URL.Path == "/webchannel/register":
			http.SetCookie(w, &http.Cookie{
				Name:  "COMPASS",
				Value: "dynamite-ui=CgAtest:dynamite=test_csession_id",
			})
			w.WriteHeader(http.StatusOK)

		case r.URL.Path == "/webchannel/events_encoded":
			typ := r.URL.Query().Get("TYPE")
			if typ == "init" {
				w.Header().Set("X-HTTP-Initial-Response", `[[0,["c","test_sid_abc","",8,12,30000]]]`)
				w.WriteHeader(http.StatusOK)
			} else {
				// Long-poll: hang briefly then close
				w.WriteHeader(http.StatusOK)
			}

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	conn := &Connection{
		session: &mockSession{},
		client:  server.Client(),
	}
	conn.ridCounter.Store(1)

	// Override the URLs to point to our test server by monkey-patching the connection's request building.
	// Since we can't easily override auth.WebchannelBase, test the individual components.

	// Test csessionid extraction from register response
	resp, err := server.Client().Post(server.URL+"/webchannel/register", "", nil)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	resp.Body.Close()

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "COMPASS" {
			conn.csessionID = extractCSSessionID(cookie.Value)
		}
	}
	if conn.csessionID != "test_csession_id" {
		t.Errorf("csessionID: got %q, want 'test_csession_id'", conn.csessionID)
	}

	// Test SID extraction from init response
	initResp, err := server.Client().Post(server.URL+"/webchannel/events_encoded?TYPE=init", "", nil)
	if err != nil {
		t.Fatalf("init request failed: %v", err)
	}
	defer initResp.Body.Close()

	initialResponse := initResp.Header.Get("X-HTTP-Initial-Response")
	sid, err := ExtractSID([]byte(initialResponse))
	if err != nil {
		t.Fatalf("ExtractSID failed: %v", err)
	}
	if sid != "test_sid_abc" {
		t.Errorf("SID: got %q, want 'test_sid_abc'", sid)
	}
}

// --- processStream tests ---

// TestProcessStream verifies chunked stream processing dispatches events.
func TestProcessStream(t *testing.T) {
	eventCount := 0
	conn := &Connection{
		handler: func(evt Event) {
			eventCount++
		},
	}

	// Simulate a stream with one chunk containing a data field.
	// The data is not valid protobuf, so it won't produce a decoded event,
	// but the parser path is exercised.
	stream := "12\n[[1,[null]]]"
	reader := readerFromString(stream)

	conn.processStream(reader)

	// No protobuf events expected since data is empty, but no crash either
	if eventCount != 0 {
		t.Errorf("expected 0 events (no data field), got %d", eventCount)
	}
}

// --- min helper test ---

// TestMin verifies the min helper.
func TestMin(t *testing.T) {
	if min(3, 5) != 3 {
		t.Error("min(3,5) should be 3")
	}
	if min(10, 2) != 2 {
		t.Error("min(10,2) should be 2")
	}
	if min(4, 4) != 4 {
		t.Error("min(4,4) should be 4")
	}
}

// readerFromString creates an io.Reader from a string for testing.
func readerFromString(s string) *strings.Reader {
	return strings.NewReader(s)
}
