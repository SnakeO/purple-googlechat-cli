// Package model tests conversation normalization, message extraction,
// GroupID parsing/formatting, and timestamp conversion.
package model

import (
	"testing"
	"time"

	pb "github.com/jacobchapa/gchat/internal/proto"
)

// --- ParseGroupID tests ---

// TestParseGroupIDSpace verifies "space:ID" parsing.
func TestParseGroupIDSpace(t *testing.T) {
	gid, err := ParseGroupID("space:ABC123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gid.GetSpaceId().GetSpaceId() != "ABC123" {
		t.Errorf("got %q, want 'ABC123'", gid.GetSpaceId().GetSpaceId())
	}
	if gid.GetDmId() != nil {
		t.Error("DmId should be nil for space")
	}
}

// TestParseGroupIDDM verifies "dm:ID" parsing.
func TestParseGroupIDDM(t *testing.T) {
	gid, err := ParseGroupID("dm:xyz789")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gid.GetDmId().GetDmId() != "xyz789" {
		t.Errorf("got %q, want 'xyz789'", gid.GetDmId().GetDmId())
	}
	if gid.GetSpaceId() != nil {
		t.Error("SpaceId should be nil for dm")
	}
}

// TestParseGroupIDDefault verifies bare IDs default to space.
func TestParseGroupIDDefault(t *testing.T) {
	gid, err := ParseGroupID("BARE_ID")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gid.GetSpaceId().GetSpaceId() != "BARE_ID" {
		t.Errorf("got %q, want 'BARE_ID'", gid.GetSpaceId().GetSpaceId())
	}
}

// TestParseGroupIDEmpty verifies empty string returns error.
func TestParseGroupIDEmpty(t *testing.T) {
	_, err := ParseGroupID("")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
}

// --- FormatGroupID tests ---

// TestFormatGroupIDSpace verifies space formatting.
func TestFormatGroupIDSpace(t *testing.T) {
	id := "ABC"
	gid := &pb.GroupId{SpaceId: &pb.SpaceId{SpaceId: &id}}
	if FormatGroupID(gid) != "space:ABC" {
		t.Errorf("got %q, want 'space:ABC'", FormatGroupID(gid))
	}
}

// TestFormatGroupIDDM verifies DM formatting.
func TestFormatGroupIDDM(t *testing.T) {
	id := "XYZ"
	gid := &pb.GroupId{DmId: &pb.DmId{DmId: &id}}
	if FormatGroupID(gid) != "dm:XYZ" {
		t.Errorf("got %q, want 'dm:XYZ'", FormatGroupID(gid))
	}
}

// TestFormatGroupIDNil verifies nil GroupId returns "unknown".
func TestFormatGroupIDNil(t *testing.T) {
	gid := &pb.GroupId{}
	if FormatGroupID(gid) != "unknown" {
		t.Errorf("got %q, want 'unknown'", FormatGroupID(gid))
	}
}

// --- ParseGroupID ↔ FormatGroupID roundtrip ---

// TestGroupIDRoundtrip verifies parse then format returns the original string.
func TestGroupIDRoundtrip(t *testing.T) {
	for _, input := range []string{"space:AAQAzTFgX1Q", "dm:k8pXsYAAAAE"} {
		gid, err := ParseGroupID(input)
		if err != nil {
			t.Fatalf("parse %q: %v", input, err)
		}
		output := FormatGroupID(gid)
		if output != input {
			t.Errorf("roundtrip: got %q, want %q", output, input)
		}
	}
}

// --- ConversationFromWorldItem tests ---

// TestConversationFromWorldItemSpace verifies Space conversion.
func TestConversationFromWorldItemSpace(t *testing.T) {
	spaceID := "SPACE_1"
	roomName := "My Room"
	item := &pb.WorldItemLite{
		GroupId:  &pb.GroupId{SpaceId: &pb.SpaceId{SpaceId: &spaceID}},
		RoomName: &roomName,
	}

	conv := ConversationFromWorldItem(item)
	if conv.ID != "SPACE_1" {
		t.Errorf("ID: got %q, want 'SPACE_1'", conv.ID)
	}
	if conv.Name != "My Room" {
		t.Errorf("Name: got %q, want 'My Room'", conv.Name)
	}
	if conv.IsDM {
		t.Error("IsDM should be false for space")
	}
}

// TestConversationFromWorldItemDM verifies DM conversion with fallback name.
func TestConversationFromWorldItemDM(t *testing.T) {
	dmID := "DM_1"
	item := &pb.WorldItemLite{
		GroupId: &pb.GroupId{DmId: &pb.DmId{DmId: &dmID}},
	}

	conv := ConversationFromWorldItem(item)
	if conv.ID != "DM_1" {
		t.Errorf("ID: got %q, want 'DM_1'", conv.ID)
	}
	if !conv.IsDM {
		t.Error("IsDM should be true for dm")
	}
	// Without RoomName or DmMembers, Name falls back to ID
	if conv.Name != "DM_1" {
		t.Errorf("Name: got %q, want 'DM_1' (fallback)", conv.Name)
	}
}

// TestConversationFromWorldItemWithMessage verifies last message extraction.
func TestConversationFromWorldItemWithMessage(t *testing.T) {
	spaceID := "S1"
	roomName := "Chat"
	text := "hello"
	createTime := int64(1716200000000000)
	item := &pb.WorldItemLite{
		GroupId:  &pb.GroupId{SpaceId: &pb.SpaceId{SpaceId: &spaceID}},
		RoomName: &roomName,
		Message: &pb.Message{
			TextBody:   &text,
			CreateTime: &createTime,
		},
	}

	conv := ConversationFromWorldItem(item)
	if conv.LastMsg != "hello" {
		t.Errorf("LastMsg: got %q, want 'hello'", conv.LastMsg)
	}
	if conv.LastTime.IsZero() {
		t.Error("LastTime should not be zero")
	}
}

// --- MessageFromProto tests ---

// TestMessageFromProtoFull verifies full message conversion.
func TestMessageFromProtoFull(t *testing.T) {
	text := "test message"
	msgID := "msg_123"
	senderName := "Alice"
	senderUID := "uid_alice"
	createTime := int64(1716200000000000)
	userType := pb.UserType_HUMAN

	msg := &pb.Message{
		Id:       &pb.MessageId{MessageId: &msgID},
		TextBody: &text,
		Creator: &pb.User{
			Name:   &senderName,
			UserId: &pb.UserId{Id: &senderUID, Type: &userType},
		},
		CreateTime: &createTime,
	}

	m := MessageFromProto(msg)
	if m.ID != "msg_123" {
		t.Errorf("ID: got %q, want 'msg_123'", m.ID)
	}
	if m.Text != "test message" {
		t.Errorf("Text: got %q, want 'test message'", m.Text)
	}
	if m.Sender != "Alice" {
		t.Errorf("Sender: got %q, want 'Alice'", m.Sender)
	}
	if m.SenderID != "uid_alice" {
		t.Errorf("SenderID: got %q, want 'uid_alice'", m.SenderID)
	}
	if m.IsDeleted {
		t.Error("IsDeleted should be false")
	}
}

// TestMessageFromProtoDeleted verifies deleted message detection.
func TestMessageFromProtoDeleted(t *testing.T) {
	deleteTime := int64(1716200000000000)
	msg := &pb.Message{
		DeleteTime: &deleteTime,
	}

	m := MessageFromProto(msg)
	if !m.IsDeleted {
		t.Error("IsDeleted should be true when DeleteTime is set")
	}
}

// TestMessageFromProtoMinimal verifies handling of minimal messages.
func TestMessageFromProtoMinimal(t *testing.T) {
	msg := &pb.Message{}
	m := MessageFromProto(msg)
	if m.Text != "" {
		t.Errorf("Text should be empty, got %q", m.Text)
	}
	if m.Sender != "" {
		t.Errorf("Sender should be empty, got %q", m.Sender)
	}
}

// --- microsToTime tests ---

// TestMicrosToTime verifies microsecond timestamp conversion.
func TestMicrosToTime(t *testing.T) {
	ts := int64(1716200000000000)
	result := microsToTime(ts)

	expected := time.Unix(1716200000, 0)
	if !result.Equal(expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

// TestMicrosToTimeWithFraction verifies sub-second precision.
func TestMicrosToTimeWithFraction(t *testing.T) {
	ts := int64(1716200000500000) // 500ms
	result := microsToTime(ts)

	if result.Nanosecond() != 500000000 {
		t.Errorf("nanosecond: got %d, want 500000000", result.Nanosecond())
	}
}

// --- truncate tests ---

// TestTruncateModel verifies truncation behavior.
func TestTruncateModel(t *testing.T) {
	if truncate("short", 80) != "short" {
		t.Error("short string should not be truncated")
	}

	long := "this is a very long message that exceeds the limit"
	result := truncate(long, 20)
	if len(result) != 23 { // 20 chars + "..."
		t.Errorf("truncated length: got %d, want 23", len(result))
	}
}
