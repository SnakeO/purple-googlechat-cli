// channel.go manages the webchannel long-poll connection to Google Chat.
// The webchannel provides real-time event streaming for incoming messages,
// typing indicators, presence changes, and other events.
package channel

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/jacobchapa/gchat/internal/auth"
	pb "github.com/jacobchapa/gchat/internal/proto"
	"google.golang.org/protobuf/proto"
)

// Event represents a decoded webchannel event.
type Event struct {
	AID      int64
	Proto    *pb.StreamEventsResponse
	RawData  []byte
}

// EventHandler is called for each decoded streaming event.
type EventHandler func(Event)

// Connection manages a webchannel long-poll session.
type Connection struct {
	session    auth.Session
	client     *http.Client
	sid        string
	csessionID string
	lastAID    int64
	ridCounter atomic.Int64
	handler    EventHandler
}

// NewConnection creates a new webchannel connection.
func NewConnection(session auth.Session, handler EventHandler) *Connection {
	c := &Connection{
		session: session,
		client:  &http.Client{},
		handler: handler,
	}
	c.ridCounter.Store(1)
	return c
}

// Connect establishes the webchannel: registers, gets SID, then long-polls.
func (c *Connection) Connect() error {
	if err := c.register(); err != nil {
		return fmt.Errorf("channel: register failed: %w", err)
	}

	if err := c.fetchSID(); err != nil {
		return fmt.Errorf("channel: SID fetch failed: %w", err)
	}

	return c.longPoll()
}

// register sends the initial webchannel registration request.
// Sets the COMPASS cookie with the csessionid.
func (c *Connection) register() error {
	reqURL := auth.WebchannelBase + "register"

	req, err := http.NewRequest("POST", reqURL, strings.NewReader(""))
	if err != nil {
		return err
	}

	if err := c.session.SetHeaders(req); err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	// Extract csessionid from COMPASS cookie in response
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "COMPASS" {
			c.csessionID = extractCSSessionID(cookie.Value)
		}
	}

	return nil
}

// extractCSSessionID pulls the csessionid from a COMPASS cookie value.
// Format: "dynamite=<csessionid>" or "dynamite-ui=...:dynamite=<csessionid>"
func extractCSSessionID(compass string) string {
	for _, part := range strings.Split(compass, ":") {
		if strings.HasPrefix(part, "dynamite=") {
			return part[len("dynamite="):]
		}
	}
	return ""
}

// fetchSID gets the session ID by sending the init request.
func (c *Connection) fetchSID() error {
	params := url.Values{
		"VER":   {"8"},
		"RID":   {"0"},
		"CVER":  {"22"},
		"TYPE":  {"init"},
		"t":     {"1"},
		"SID":   {"null"},
	}

	reqURL := auth.WebchannelBase + "events_encoded?" + params.Encode()
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(""))
	if err != nil {
		return err
	}

	if err := c.session.SetHeaders(req); err != nil {
		return err
	}
	if c.csessionID != "" {
		req.AddCookie(&http.Cookie{Name: "COMPASS", Value: "dynamite=" + c.csessionID})
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// SID is in the X-HTTP-Initial-Response header
	initialResp := resp.Header.Get("X-HTTP-Initial-Response")
	if initialResp != "" {
		sid, err := ExtractSID([]byte(initialResp))
		if err != nil {
			return err
		}
		c.sid = sid
		return nil
	}

	// Fallback: read from body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	sid, err := ExtractSID(body)
	if err != nil {
		return err
	}
	c.sid = sid
	return nil
}

// longPoll starts the streaming long-poll connection.
// Reads chunked responses and dispatches events to the handler.
func (c *Connection) longPoll() error {
	for {
		if err := c.pollOnce(); err != nil {
			return err
		}
	}
}

// pollOnce makes a single long-poll request and processes events.
func (c *Connection) pollOnce() error {
	rid := c.ridCounter.Add(1)
	params := url.Values{
		"VER":  {"8"},
		"RID":  {"rpc"},
		"SID":  {c.sid},
		"AID":  {strconv.FormatInt(c.lastAID, 10)},
		"TYPE": {"xmlhttp"},
		"CI":   {"0"},
		"t":    {"1"},
	}

	reqURL := auth.WebchannelBase + "events_encoded?" + params.Encode()
	_ = rid

	req, err := http.NewRequest("POST", reqURL, strings.NewReader(""))
	if err != nil {
		return err
	}

	if err := c.session.SetHeaders(req); err != nil {
		return err
	}
	if c.csessionID != "" {
		req.AddCookie(&http.Cookie{Name: "COMPASS", Value: "dynamite=" + c.csessionID})
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("channel: long-poll request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("channel: long-poll returned %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}

	return c.processStream(resp.Body)
}

// processStream reads and processes a chunked long-poll response.
func (c *Connection) processStream(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var buffer strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		buffer.WriteString(line)
		buffer.WriteString("\n")

		chunks, err := ParseChunks([]byte(buffer.String()))
		if err != nil {
			continue
		}

		if len(chunks) > 0 {
			for _, chunk := range chunks {
				c.handleChunk(chunk)
			}
			buffer.Reset()
		}
	}

	return scanner.Err()
}

// handleChunk processes a single webchannel chunk.
func (c *Connection) handleChunk(chunk Chunk) {
	if chunk.AID > c.lastAID {
		c.lastAID = chunk.AID
	}

	if len(chunk.Data) == 0 {
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(string(chunk.Data))
	if err != nil {
		return
	}

	streamResp := &pb.StreamEventsResponse{}
	if err := proto.Unmarshal(decoded, streamResp); err != nil {
		return
	}

	c.handler(Event{
		AID:     chunk.AID,
		Proto:   streamResp,
		RawData: decoded,
	})
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
