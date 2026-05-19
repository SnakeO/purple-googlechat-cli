// Package channel implements the Google Chat webchannel long-poll streaming connection.
// The webchannel uses a chunked HTTP response format where each chunk is a
// length-prefixed JSON array containing base64-encoded protobuf events.
package channel

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Chunk represents a single parsed webchannel chunk.
type Chunk struct {
	AID  int64
	Data []byte // base64-decoded protobuf, or nil for non-data chunks
	Raw  json.RawMessage
}

// ParseChunks splits a webchannel response body into individual chunks.
// Format: <decimal_length>\n<json_data><decimal_length>\n<json_data>...
func ParseChunks(data []byte) ([]Chunk, error) {
	var chunks []Chunk
	remaining := string(data)

	for len(remaining) > 0 {
		remaining = strings.TrimLeft(remaining, " \t\r\n")
		if len(remaining) == 0 {
			break
		}

		newlineIdx := strings.Index(remaining, "\n")
		if newlineIdx == -1 {
			break
		}

		lengthStr := strings.TrimSpace(remaining[:newlineIdx])
		chunkLen, err := strconv.Atoi(lengthStr)
		if err != nil {
			return chunks, fmt.Errorf("channel: invalid chunk length %q: %w", lengthStr, err)
		}

		remaining = remaining[newlineIdx+1:]
		if len(remaining) < chunkLen {
			break
		}

		chunkData := remaining[:chunkLen]
		remaining = remaining[chunkLen:]

		parsed, err := parseChunkJSON([]byte(chunkData))
		if err != nil {
			continue
		}
		chunks = append(chunks, parsed...)
	}

	return chunks, nil
}

// parseChunkJSON parses a single JSON chunk into Chunk structs.
// Format: [[aid, [event_data]], [aid2, [event_data2]], ...]
func parseChunkJSON(data []byte) ([]Chunk, error) {
	var outer []json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		return nil, fmt.Errorf("channel: invalid chunk JSON: %w", err)
	}

	var chunks []Chunk
	for _, elem := range outer {
		var inner []json.RawMessage
		if err := json.Unmarshal(elem, &inner); err != nil {
			continue
		}
		if len(inner) < 2 {
			continue
		}

		var aid int64
		json.Unmarshal(inner[0], &aid)

		chunk := Chunk{AID: aid, Raw: inner[1]}

		// Try to extract base64-encoded protobuf data
		var events []json.RawMessage
		if json.Unmarshal(inner[1], &events) == nil && len(events) > 0 {
			// Check if first element is an object with "data" field
			if isNull(events[0]) && len(events) > 1 {
				chunk.Data = extractDataField(events[1])
			} else {
				chunk.Data = extractDataField(events[0])
			}
		}

		chunks = append(chunks, chunk)
	}

	return chunks, nil
}

// extractDataField extracts the base64 "data" field from a JSON object.
func extractDataField(elem json.RawMessage) []byte {
	var obj map[string]json.RawMessage
	if json.Unmarshal(elem, &obj) != nil {
		return nil
	}

	dataField, ok := obj["data"]
	if !ok {
		return nil
	}

	var b64 string
	if json.Unmarshal(dataField, &b64) != nil {
		return nil
	}

	return []byte(b64)
}

// isNull returns true if the JSON element is a literal null.
func isNull(data json.RawMessage) bool {
	return len(data) == 4 && string(data) == "null"
}

// ExtractSID extracts the session ID from the initial webchannel response.
// Format: [[0,["c","<SID>","",8,12,30000]]]
func ExtractSID(data []byte) (string, error) {
	var outer []json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		return "", fmt.Errorf("channel: invalid SID response: %w", err)
	}
	if len(outer) == 0 {
		return "", fmt.Errorf("channel: empty SID response")
	}

	var inner []json.RawMessage
	if err := json.Unmarshal(outer[0], &inner); err != nil {
		return "", fmt.Errorf("channel: invalid SID inner array: %w", err)
	}
	if len(inner) < 2 {
		return "", fmt.Errorf("channel: SID inner array too short")
	}

	var payload []json.RawMessage
	if err := json.Unmarshal(inner[1], &payload); err != nil {
		return "", fmt.Errorf("channel: invalid SID payload: %w", err)
	}
	if len(payload) < 2 {
		return "", fmt.Errorf("channel: SID payload too short")
	}

	var sid string
	if err := json.Unmarshal(payload[1], &sid); err != nil {
		return "", fmt.Errorf("channel: invalid SID string: %w", err)
	}

	return sid, nil
}
