// Package cache tests all CRUD operations, upsert-overwrites, and query logic
// for the local SQLite cache: conversations, messages, users, memberships.
package cache

import (
	"testing"
	"time"
)

// helper creates a temp cache DB for testing.
func testCache(t *testing.T) *Cache {
	t.Helper()
	dbPath := t.TempDir() + "/test.db"
	c, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// --- Conversation tests ---

// TestUpsertConversation verifies insert and update of conversations.
func TestUpsertConversation(t *testing.T) {
	c := testCache(t)

	err := c.UpsertConversation("dm:abc", "Alice", true, "hello", time.Now().UnixMicro())
	if err != nil {
		t.Fatalf("UpsertConversation: %v", err)
	}

	convos, err := c.ListConversations(0)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(convos) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convos))
	}
	if convos[0].ID != "dm:abc" {
		t.Errorf("ID: got %q, want 'dm:abc'", convos[0].ID)
	}
	if convos[0].Name != "Alice" {
		t.Errorf("Name: got %q, want 'Alice'", convos[0].Name)
	}
}

// TestUpsertConversationOverwrite verifies update-on-conflict.
func TestUpsertConversationOverwrite(t *testing.T) {
	c := testCache(t)

	c.UpsertConversation("dm:abc", "Alice", true, "hi", 100)
	c.UpsertConversation("dm:abc", "Alice B", true, "bye", 200)

	convos, _ := c.ListConversations(0)
	if len(convos) != 1 {
		t.Fatalf("expected 1 after upsert, got %d", len(convos))
	}
	if convos[0].Name != "Alice B" {
		t.Errorf("Name not updated: got %q", convos[0].Name)
	}
	if convos[0].LastMsg != "bye" {
		t.Errorf("LastMsg not updated: got %q", convos[0].LastMsg)
	}
}

// TestListConversationsLimit verifies the limit parameter.
func TestListConversationsLimit(t *testing.T) {
	c := testCache(t)

	c.UpsertConversation("dm:a", "A", true, "", 0)
	c.UpsertConversation("dm:b", "B", true, "", 0)
	c.UpsertConversation("dm:c", "C", true, "", 0)

	convos, _ := c.ListConversations(2)
	if len(convos) != 2 {
		t.Errorf("expected 2 with limit, got %d", len(convos))
	}
}

// --- Message tests ---

// TestUpsertMessage verifies insert and retrieval of messages.
func TestUpsertMessage(t *testing.T) {
	c := testCache(t)

	err := c.UpsertMessage("dm:abc", "msg1", "user1", "hello world", time.Now().UnixMicro(), false)
	if err != nil {
		t.Fatalf("UpsertMessage: %v", err)
	}

	msgs, err := c.ListMessages("dm:abc", 0, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Text != "hello world" {
		t.Errorf("Text: got %q", msgs[0].Text)
	}
}

// TestUpsertMessageOverwrite verifies edited messages update text.
func TestUpsertMessageOverwrite(t *testing.T) {
	c := testCache(t)

	c.UpsertMessage("dm:abc", "msg1", "user1", "original", 100, false)
	c.UpsertMessage("dm:abc", "msg1", "user1", "edited", 100, false)

	msgs, _ := c.ListMessages("dm:abc", 0, 0)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 after upsert, got %d", len(msgs))
	}
	if msgs[0].Text != "edited" {
		t.Errorf("Text not updated: got %q", msgs[0].Text)
	}
}

// TestMarkDeleted verifies soft-delete of messages.
func TestMarkDeleted(t *testing.T) {
	c := testCache(t)

	c.UpsertMessage("dm:abc", "msg1", "user1", "to delete", 100, false)
	c.MarkDeleted("dm:abc", "msg1")

	msgs, _ := c.ListMessages("dm:abc", 0, 0)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if !msgs[0].IsDeleted {
		t.Error("expected message to be marked deleted")
	}
}

// TestListMessagesSince verifies time-filtered queries.
func TestListMessagesSince(t *testing.T) {
	c := testCache(t)

	c.UpsertMessage("dm:abc", "old", "u1", "old msg", 100, false)
	c.UpsertMessage("dm:abc", "new", "u1", "new msg", 500, false)

	msgs, _ := c.ListMessages("dm:abc", 0, 300)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message since 300, got %d", len(msgs))
	}
	if msgs[0].MessageID != "new" {
		t.Errorf("expected 'new', got %q", msgs[0].MessageID)
	}
}

// TestListMessagesLimit verifies message count limiting.
func TestListMessagesLimit(t *testing.T) {
	c := testCache(t)

	for i := 0; i < 10; i++ {
		c.UpsertMessage("dm:abc", string(rune('a'+i)), "u1", "msg", int64(i*100), false)
	}

	msgs, _ := c.ListMessages("dm:abc", 3, 0)
	if len(msgs) != 3 {
		t.Errorf("expected 3 with limit, got %d", len(msgs))
	}
}

// --- User tests ---

// TestUpsertUser verifies insert and retrieval of users.
func TestUpsertUser(t *testing.T) {
	c := testCache(t)

	err := c.UpsertUser("gaia123", "Alice", "alice@test.com")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	u, err := c.GetUser("gaia123")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u == nil {
		t.Fatal("expected user, got nil")
	}
	if u.Name != "Alice" {
		t.Errorf("Name: got %q", u.Name)
	}
}

// TestUpsertUserOverwrite verifies name updates.
func TestUpsertUserOverwrite(t *testing.T) {
	c := testCache(t)

	c.UpsertUser("gaia123", "Alice", "alice@test.com")
	c.UpsertUser("gaia123", "Alice B", "alice.b@test.com")

	u, _ := c.GetUser("gaia123")
	if u.Name != "Alice B" {
		t.Errorf("Name not updated: got %q", u.Name)
	}
}

// TestGetUserNotFound verifies nil return for missing users.
func TestGetUserNotFound(t *testing.T) {
	c := testCache(t)

	u, err := c.GetUser("nonexistent")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if u != nil {
		t.Errorf("expected nil for missing user, got %+v", u)
	}
}

// --- Membership tests ---

// TestUpsertMembership verifies membership storage.
func TestUpsertMembership(t *testing.T) {
	c := testCache(t)

	err := c.UpsertMembership("dm:abc", "gaia123")
	if err != nil {
		t.Fatalf("UpsertMembership: %v", err)
	}

	err = c.UpsertMembership("dm:abc", "gaia456")
	if err != nil {
		t.Fatalf("UpsertMembership: %v", err)
	}

	members, err := c.ListMemberships("dm:abc")
	if err != nil {
		t.Fatalf("ListMemberships: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}
}

// TestGetDMContactName verifies joining memberships with users, excluding self.
func TestGetDMContactName(t *testing.T) {
	c := testCache(t)

	c.UpsertUser("self", "Me", "me@test.com")
	c.UpsertUser("other", "Bob", "bob@test.com")
	c.UpsertMembership("dm:abc", "self")
	c.UpsertMembership("dm:abc", "other")

	name, err := c.GetDMContactName("dm:abc", "self")
	if err != nil {
		t.Fatalf("GetDMContactName: %v", err)
	}
	if name != "Bob" {
		t.Errorf("expected 'Bob', got %q", name)
	}
}

// --- Cache meta tests ---

// TestSetGetMeta verifies key-value metadata storage.
func TestSetGetMeta(t *testing.T) {
	c := testCache(t)

	err := c.SetMeta("self_gaia_id", "12345")
	if err != nil {
		t.Fatalf("SetMeta: %v", err)
	}

	val, err := c.GetMeta("self_gaia_id")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if val != "12345" {
		t.Errorf("got %q, want '12345'", val)
	}
}

// TestGetMetaNotFound verifies empty return for missing keys.
func TestGetMetaNotFound(t *testing.T) {
	c := testCache(t)

	val, err := c.GetMeta("nonexistent")
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty, got %q", val)
	}
}

// --- Schema tests ---

// TestOpenCreatesSchema verifies tables exist after Open.
func TestOpenCreatesSchema(t *testing.T) {
	c := testCache(t)

	var count int
	err := c.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('conversations','messages','users','memberships','cache_meta')").Scan(&count)
	if err != nil {
		t.Fatalf("schema query: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 tables, found %d", count)
	}
}

// TestOpenIdempotent verifies re-opening an existing DB doesn't fail.
func TestOpenIdempotent(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"

	c1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	c1.Close()

	c2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	c2.Close()
}
