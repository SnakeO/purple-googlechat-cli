// since_test.go tests the flexible --since time parser.
package model

import (
	"testing"
	"time"
)

// TestParseSinceDuration verifies Go duration parsing.
func TestParseSinceDuration(t *testing.T) {
	result, err := ParseSince("168h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Now().Add(-168 * time.Hour)
	diff := result.Sub(expected)
	if diff > time.Second || diff < -time.Second {
		t.Errorf("result off by %v", diff)
	}
}

// TestParseSinceRFC3339 verifies RFC3339 datetime parsing.
func TestParseSinceRFC3339(t *testing.T) {
	result, err := ParseSince("2024-01-15T10:30:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Year() != 2024 || result.Month() != 1 || result.Day() != 15 {
		t.Errorf("got %v", result)
	}
}

// TestParseSinceDate verifies YYYY-MM-DD parsing.
func TestParseSinceDate(t *testing.T) {
	result, err := ParseSince("2025-06-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Year() != 2025 || result.Month() != 6 || result.Day() != 1 {
		t.Errorf("got %v", result)
	}
}

// TestParseSinceEmpty verifies empty string returns zero time.
func TestParseSinceEmpty(t *testing.T) {
	result, err := ParseSince("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero time, got %v", result)
	}
}

// TestParseSinceInvalid verifies error for garbage input.
func TestParseSinceInvalid(t *testing.T) {
	_, err := ParseSince("not-a-time")
	if err == nil {
		t.Fatal("expected error for invalid input")
	}
}
