// Package model provides normalized application types for Google Chat.
// These types decouple the CLI/TUI from the raw protobuf wire format,
// unifying DMs and Spaces behind a single Conversation abstraction.
package model

import (
	"fmt"
	"strings"
	"time"

	pb "github.com/jacobchapa/gchat/internal/proto"
)

// Conversation represents a unified chat (DM or Space).
type Conversation struct {
	ID       string // Space ID or DM ID
	Name     string // Display name
	IsDM     bool
	GroupID  *pb.GroupId
	LastMsg  string
	LastTime time.Time
}

// Message represents a single chat message.
type Message struct {
	ID          string
	Sender      string
	SenderID    string
	Text        string
	Time        time.Time
	IsDeleted   bool
	Attachments []Attachment
}

// Attachment represents a file attached to a message.
type Attachment struct {
	Token       string // attachment_token for download
	Name        string // content_name (filename)
	ContentType string // MIME type
	DriveID     string // cloned_drive_id if uploaded to Drive
}

// User represents the authenticated user's identity.
type User struct {
	GaiaID string
	Name   string
	Email  string
}

// ConversationFromWorldItem converts a protobuf WorldItemLite to a Conversation.
func ConversationFromWorldItem(item *pb.WorldItemLite) Conversation {
	conv := Conversation{
		GroupID: item.GetGroupId(),
	}

	// Extract ID — prefer space_id, fall back to dm_id
	if sid := item.GetGroupId().GetSpaceId(); sid != nil {
		conv.ID = sid.GetSpaceId()
		conv.IsDM = false
	} else if did := item.GetGroupId().GetDmId(); did != nil {
		conv.ID = did.GetDmId()
		conv.IsDM = true
	}

	// Display name
	if item.GetRoomName() != "" {
		conv.Name = item.GetRoomName()
	} else if item.GetDmMembers() != nil {
		conv.Name = formatDMMembers(item)
	} else {
		conv.Name = conv.ID
	}

	// Last message preview
	if msg := item.GetMessage(); msg != nil {
		conv.LastMsg = truncate(msg.GetTextBody(), 80)
		if msg.GetCreateTime() != 0 {
			conv.LastTime = microsToTime(msg.GetCreateTime())
		}
	}

	return conv
}

// MessageFromProto converts a protobuf Message to a model Message.
func MessageFromProto(msg *pb.Message) Message {
	m := Message{
		Text:      msg.GetTextBody(),
		IsDeleted: msg.GetDeleteTime() != 0,
	}

	if mid := msg.GetId(); mid != nil {
		m.ID = mid.GetMessageId()
	}

	if creator := msg.GetCreator(); creator != nil {
		m.Sender = creator.GetName()
		if uid := creator.GetUserId(); uid != nil {
			m.SenderID = uid.GetId()
		}
	}

	if msg.GetCreateTime() != 0 {
		m.Time = microsToTime(msg.GetCreateTime())
	}

	for _, ann := range msg.GetAnnotations() {
		if um := ann.GetUploadMetadata(); um != nil {
			m.Attachments = append(m.Attachments, Attachment{
				Token:       um.GetAttachmentToken(),
				Name:        um.GetContentName(),
				ContentType: um.GetContentType(),
				DriveID:     um.GetClonedDriveId(),
			})
		}
	}

	return m
}

// formatDMMembers builds a display name from DM member info.
func formatDMMembers(item *pb.WorldItemLite) string {
	if nu := item.GetNameUsers(); nu != nil {
		if gn := nu.GetGroupName(); gn != "" {
			return gn
		}
		ids := nu.GetNameUserIds()
		if len(ids) > 0 {
			result := ""
			for i, uid := range ids {
				if i > 0 {
					result += ", "
				}
				result += uid.GetId()
			}
			return result
		}
	}
	return "DM"
}

// microsToTime converts a microsecond timestamp to time.Time.
func microsToTime(micros int64) time.Time {
	return time.Unix(micros/1_000_000, (micros%1_000_000)*1000)
}

// FirstLine returns the first line of a string, adding "..." if there are more lines.
func FirstLine(s string) string {
	idx := strings.Index(s, "\n")
	if idx == -1 {
		return s
	}
	return s[:idx] + " ..."
}

// truncate shortens a string to maxLen.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// FormatGroupID returns a human-readable string from a GroupId.
func FormatGroupID(gid *pb.GroupId) string {
	if sid := gid.GetSpaceId(); sid != nil {
		return fmt.Sprintf("space:%s", sid.GetSpaceId())
	}
	if did := gid.GetDmId(); did != nil {
		return fmt.Sprintf("dm:%s", did.GetDmId())
	}
	return "unknown"
}

// ParseGroupID parses a "space:ID" or "dm:ID" string into a GroupId.
func ParseGroupID(s string) (*pb.GroupId, error) {
	if strings.HasPrefix(s, "space:") {
		id := s[6:]
		return &pb.GroupId{SpaceId: &pb.SpaceId{SpaceId: &id}}, nil
	}

	if strings.HasPrefix(s, "dm:") {
		id := s[3:]
		return &pb.GroupId{DmId: &pb.DmId{DmId: &id}}, nil
	}

	if s == "" {
		return nil, fmt.Errorf("empty group ID")
	}

	// Default: treat as space ID
	return &pb.GroupId{SpaceId: &pb.SpaceId{SpaceId: &s}}, nil
}
