// Package api tests the typed API wrappers: request construction,
// request header generation, and response handling.
package api

import (
	"testing"

	pb "github.com/jacobchapa/gchat/internal/proto"
)

// --- NewRequestHeader tests ---

// TestNewRequestHeader verifies default request header values.
func TestNewRequestHeader(t *testing.T) {
	hdr := NewRequestHeader()

	if hdr.GetClientType() != pb.RequestHeader_IOS {
		t.Errorf("ClientType: got %v, want IOS", hdr.GetClientType())
	}
	if hdr.GetTraceId() != 0 {
		t.Errorf("TraceId: got %d, want 0", hdr.GetTraceId())
	}
}

// --- Request construction tests ---

// TestGetSelfUserStatusRequestConstruction verifies the request is properly formed.
func TestGetSelfUserStatusRequestConstruction(t *testing.T) {
	hdr := NewRequestHeader()
	req := &pb.GetSelfUserStatusRequest{
		RequestHeader: hdr,
	}

	if req.GetRequestHeader().GetClientType() != pb.RequestHeader_IOS {
		t.Error("request header not set correctly")
	}
}

// TestPaginatedWorldRequestConstruction verifies world request includes required fields.
func TestPaginatedWorldRequestConstruction(t *testing.T) {
	pageSize := int32(999)
	fetchSpaces := true
	fetchSnippets := true
	req := &pb.PaginatedWorldRequest{
		RequestHeader:                NewRequestHeader(),
		FetchFromUserSpaces:          &fetchSpaces,
		FetchSnippetsForUnnamedRooms: &fetchSnippets,
		WorldSectionRequests: []*pb.WorldSectionRequest{
			{PageSize: &pageSize},
		},
	}

	if !req.GetFetchFromUserSpaces() {
		t.Error("FetchFromUserSpaces should be true")
	}
	if !req.GetFetchSnippetsForUnnamedRooms() {
		t.Error("FetchSnippetsForUnnamedRooms should be true")
	}
	if len(req.GetWorldSectionRequests()) != 1 {
		t.Fatal("expected 1 WorldSectionRequest")
	}
	if req.GetWorldSectionRequests()[0].GetPageSize() != 999 {
		t.Errorf("PageSize: got %d, want 999", req.GetWorldSectionRequests()[0].GetPageSize())
	}
}

// TestCatchUpGroupRequestConstruction verifies catch-up request with range and pagination.
func TestCatchUpGroupRequestConstruction(t *testing.T) {
	pageSize := int32(500)
	cutoffSize := int32(500)
	fromTS := int64(1716200000000000)
	dmID := "test_dm"

	req := &pb.CatchUpGroupRequest{
		RequestHeader: NewRequestHeader(),
		Range: &pb.CatchUpRange{
			FromRevisionTimestamp: &fromTS,
		},
		GroupId:    &pb.GroupId{DmId: &pb.DmId{DmId: &dmID}},
		PageSize:   &pageSize,
		CutoffSize: &cutoffSize,
	}

	if req.GetRange().GetFromRevisionTimestamp() != 1716200000000000 {
		t.Error("FromRevisionTimestamp not set")
	}
	if req.GetGroupId().GetDmId().GetDmId() != "test_dm" {
		t.Error("GroupId DmId not set")
	}
	if req.GetPageSize() != 500 {
		t.Errorf("PageSize: got %d, want 500", req.GetPageSize())
	}
}

// TestCreateTopicRequestConstruction verifies message send request.
func TestCreateTopicRequestConstruction(t *testing.T) {
	text := "hello world"
	localID := "gchat_123"
	spaceID := "SPACE_1"

	req := &pb.CreateTopicRequest{
		RequestHeader: NewRequestHeader(),
		GroupId:       &pb.GroupId{SpaceId: &pb.SpaceId{SpaceId: &spaceID}},
		TextBody:      &text,
		LocalId:       &localID,
	}

	if req.GetTextBody() != "hello world" {
		t.Errorf("TextBody: got %q", req.GetTextBody())
	}
	if req.GetLocalId() != "gchat_123" {
		t.Errorf("LocalId: got %q", req.GetLocalId())
	}
	if req.GetGroupId().GetSpaceId().GetSpaceId() != "SPACE_1" {
		t.Error("GroupId SpaceId not set")
	}
}

// TestListMembersRequestConstruction verifies member listing request.
func TestListMembersRequestConstruction(t *testing.T) {
	pageSize := int32(100)
	dmID := "dm_abc"

	req := &pb.ListMembersRequest{
		RequestHeader: NewRequestHeader(),
		GroupId:       &pb.GroupId{DmId: &pb.DmId{DmId: &dmID}},
		PageSize:      &pageSize,
	}

	if req.GetGroupId().GetDmId().GetDmId() != "dm_abc" {
		t.Error("GroupId not set")
	}
	if req.GetPageSize() != 100 {
		t.Errorf("PageSize: got %d, want 100", req.GetPageSize())
	}
}

// TestCatchUpUserRequestConstruction verifies user-level catch-up request.
func TestCatchUpUserRequestConstruction(t *testing.T) {
	pageSize := int32(100)
	cutoffSize := int32(100)
	fromTS := int64(0)

	req := &pb.CatchUpUserRequest{
		RequestHeader: NewRequestHeader(),
		Range: &pb.CatchUpRange{
			FromRevisionTimestamp: &fromTS,
		},
		PageSize:   &pageSize,
		CutoffSize: &cutoffSize,
	}

	if req.GetRange().GetFromRevisionTimestamp() != 0 {
		t.Error("FromRevisionTimestamp should be 0")
	}
	if req.GetPageSize() != 100 {
		t.Errorf("PageSize: got %d, want 100", req.GetPageSize())
	}
}
