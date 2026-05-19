// Package api provides typed wrappers for Google Chat's internal API endpoints.
// Each function maps to one /api/* endpoint and handles protobuf serialization.
package api

import (
	"fmt"

	pb "github.com/jacobchapa/gchat/internal/proto"
	"github.com/jacobchapa/gchat/internal/transport"
)

// ChatAPI wraps the transport client with typed API methods.
type ChatAPI struct {
	client *transport.Client
}

// New creates a new ChatAPI instance.
func New(client *transport.Client) *ChatAPI {
	return &ChatAPI{client: client}
}

// GetSelfUserStatus fetches the authenticated user's status and Gaia ID.
func (a *ChatAPI) GetSelfUserStatus(reqHeader *pb.RequestHeader) (*pb.GetSelfUserStatusResponse, error) {
	req := &pb.GetSelfUserStatusRequest{
		RequestHeader: reqHeader,
	}
	resp := &pb.GetSelfUserStatusResponse{}

	if err := a.client.APIRequest("/api/get_self_user_status", req, resp); err != nil {
		return nil, fmt.Errorf("api: get_self_user_status: %w", err)
	}
	return resp, nil
}

// PaginatedWorld fetches the conversation list.
func (a *ChatAPI) PaginatedWorld(reqHeader *pb.RequestHeader) (*pb.PaginatedWorldResponse, error) {
	pageSize := int32(999)
	fetchSpaces := true
	fetchSnippets := true
	req := &pb.PaginatedWorldRequest{
		RequestHeader:               reqHeader,
		FetchFromUserSpaces:         &fetchSpaces,
		FetchSnippetsForUnnamedRooms: &fetchSnippets,
		WorldSectionRequests: []*pb.WorldSectionRequest{
			{PageSize: &pageSize},
		},
	}
	resp := &pb.PaginatedWorldResponse{}

	if err := a.client.APIRequest("/api/paginated_world", req, resp); err != nil {
		return nil, fmt.Errorf("api: paginated_world: %w", err)
	}
	return resp, nil
}

// CatchUpGroup fetches message history for a conversation.
func (a *ChatAPI) CatchUpGroup(reqHeader *pb.RequestHeader, groupID *pb.GroupId, fromTimestamp int64) (*pb.CatchUpResponse, error) {
	pageSize := int32(500)
	cutoffSize := int32(500)
	req := &pb.CatchUpGroupRequest{
		RequestHeader: reqHeader,
		Range: &pb.CatchUpRange{
			FromRevisionTimestamp: &fromTimestamp,
		},
		GroupId:    groupID,
		PageSize:   &pageSize,
		CutoffSize: &cutoffSize,
	}
	resp := &pb.CatchUpResponse{}

	if err := a.client.APIRequest("/api/catch_up_group", req, resp); err != nil {
		return nil, fmt.Errorf("api: catch_up_group: %w", err)
	}
	return resp, nil
}

// CreateTopic sends a message to a conversation.
func (a *ChatAPI) CreateTopic(reqHeader *pb.RequestHeader, groupID *pb.GroupId, text string, localID string) (*pb.CreateTopicResponse, error) {
	req := &pb.CreateTopicRequest{
		RequestHeader: reqHeader,
		GroupId:       groupID,
		TextBody:      &text,
		LocalId:       &localID,
	}
	resp := &pb.CreateTopicResponse{}

	if err := a.client.APIRequest("/api/create_topic", req, resp); err != nil {
		return nil, fmt.Errorf("api: create_topic: %w", err)
	}
	return resp, nil
}

// CatchUpUser fetches all recent events across all conversations.
func (a *ChatAPI) CatchUpUser(reqHeader *pb.RequestHeader, fromTimestamp int64) (*pb.CatchUpResponse, error) {
	pageSize := int32(100)
	cutoffSize := int32(100)
	req := &pb.CatchUpUserRequest{
		RequestHeader: reqHeader,
		Range: &pb.CatchUpRange{
			FromRevisionTimestamp: &fromTimestamp,
		},
		PageSize:   &pageSize,
		CutoffSize: &cutoffSize,
	}
	resp := &pb.CatchUpResponse{}

	if err := a.client.APIRequest("/api/catch_up_user", req, resp); err != nil {
		return nil, fmt.Errorf("api: catch_up_user: %w", err)
	}
	return resp, nil
}

// GetMembers fetches detailed member info (name, email) for given member IDs.
func (a *ChatAPI) GetMembers(reqHeader *pb.RequestHeader, memberIDs []*pb.MemberId) (*pb.GetMembersResponse, error) {
	req := &pb.GetMembersRequest{
		RequestHeader: reqHeader,
		MemberIds:     memberIDs,
	}
	resp := &pb.GetMembersResponse{}

	if err := a.client.APIRequest("/api/get_members", req, resp); err != nil {
		return nil, fmt.Errorf("api: get_members: %w", err)
	}
	return resp, nil
}

// ListMembers fetches the members of a conversation.
func (a *ChatAPI) ListMembers(reqHeader *pb.RequestHeader, groupID *pb.GroupId) (*pb.ListMembersResponse, error) {
	pageSize := int32(100)
	req := &pb.ListMembersRequest{
		RequestHeader: reqHeader,
		GroupId:       groupID,
		PageSize:      &pageSize,
	}
	resp := &pb.ListMembersResponse{}

	if err := a.client.APIRequest("/api/list_members", req, resp); err != nil {
		return nil, fmt.Errorf("api: list_members: %w", err)
	}
	return resp, nil
}

// NewRequestHeader creates a standard request header for API calls.
func NewRequestHeader() *pb.RequestHeader {
	clientType := pb.RequestHeader_IOS
	traceID := int64(0)
	return &pb.RequestHeader{
		ClientType: &clientType,
		TraceId:    &traceID,
	}
}
