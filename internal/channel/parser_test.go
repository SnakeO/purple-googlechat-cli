// Package channel tests the webchannel chunk parser and SID extraction.
package channel

import (
	"testing"
)

// TestParseChunks verifies length-prefixed chunk splitting.
func TestParseChunks(t *testing.T) {
	input := "12\n[[1,[null]]]14\n[[2,[\"test\"]]]"

	chunks, err := ParseChunks([]byte(input))
	if err != nil {
		t.Fatalf("ParseChunks failed: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].AID != 1 {
		t.Errorf("chunk 0 AID: got %d, want 1", chunks[0].AID)
	}
	if chunks[1].AID != 2 {
		t.Errorf("chunk 1 AID: got %d, want 2", chunks[1].AID)
	}
}

// TestParseChunksWithData verifies base64 data extraction from event chunks.
func TestParseChunksWithData(t *testing.T) {
	input := "32\n" + `[[1,[null,{"data":"dGVzdA=="}]]]`

	chunks, err := ParseChunks([]byte(input))
	if err != nil {
		t.Fatalf("ParseChunks failed: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if string(chunks[0].Data) != "dGVzdA==" {
		t.Errorf("data: got %q, want 'dGVzdA=='", chunks[0].Data)
	}
}

// TestExtractSID verifies session ID extraction from the init response.
func TestExtractSID(t *testing.T) {
	input := `[[0,["c","test_sid_12345","",8,12,30000]]]`

	sid, err := ExtractSID([]byte(input))
	if err != nil {
		t.Fatalf("ExtractSID failed: %v", err)
	}
	if sid != "test_sid_12345" {
		t.Errorf("SID: got %q, want 'test_sid_12345'", sid)
	}
}

// TestParseChunksIncomplete verifies graceful handling of incomplete data.
func TestParseChunksIncomplete(t *testing.T) {
	input := "100\nonly_partial_data"

	chunks, err := ParseChunks([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for incomplete data, got %d", len(chunks))
	}
}
