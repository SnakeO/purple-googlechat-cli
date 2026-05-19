// Package pblite tests the encode/decode roundtrip for the pblite codec.
// Pblite is Google's JSON-array encoding of protobuf messages, used by
// Google Chat's internal API alongside standard binary protobuf.
package pblite

import (
	"encoding/json"
	"testing"

	pb "github.com/jacobchapa/gchat/internal/proto"
	"google.golang.org/protobuf/proto"
)

// --- Encode Tests ---

// TestEncodeSimpleMessage verifies encoding a flat message with scalar fields.
func TestEncodeSimpleMessage(t *testing.T) {
	id := "12345"
	userType := pb.UserType_HUMAN
	msg := &pb.UserId{
		Id:   &id,
		Type: &userType,
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// UserId: field 1 = id (string), field 2 = type (enum/int)
	// Expected pblite: ["12345", 0]
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if len(arr) < 2 {
		t.Fatalf("expected at least 2 elements, got %d", len(arr))
	}
	if arr[0] != "12345" {
		t.Errorf("field 1: got %v, want '12345'", arr[0])
	}
	if arr[1] != float64(0) {
		t.Errorf("field 2: got %v, want 0 (HUMAN)", arr[1])
	}
}

// TestEncodeUnsetOptionalFields verifies that unset optional fields encode as null.
func TestEncodeUnsetOptionalFields(t *testing.T) {
	id := "12345"
	msg := &pb.UserId{
		Id: &id,
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	if arr[0] != "12345" {
		t.Errorf("field 1: got %v, want '12345'", arr[0])
	}
	// field 2 (type) is unset — should be null
	if arr[1] != nil {
		t.Errorf("field 2: got %v, want null", arr[1])
	}
}

// TestEncodeNestedMessage verifies that nested messages encode as nested arrays.
func TestEncodeNestedMessage(t *testing.T) {
	userId := "user1"
	userType := pb.UserType_HUMAN
	msg := &pb.MemberId{
		UserId: &pb.UserId{
			Id:   &userId,
			Type: &userType,
		},
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// MemberId field 1 = UserId, which should be a nested array ["user1", 0]
	nested, ok := arr[0].([]interface{})
	if !ok {
		t.Fatalf("field 1: expected nested array, got %T: %v", arr[0], arr[0])
	}
	if nested[0] != "user1" {
		t.Errorf("nested field 1: got %v, want 'user1'", nested[0])
	}
}

// TestEncodeRepeatedField verifies repeated fields encode as JSON arrays.
func TestEncodeRepeatedField(t *testing.T) {
	msg := &pb.GetMembersRequest{
		MemberIds: []*pb.MemberId{
			{Email: strPtr("a@test.com")},
			{Email: strPtr("b@test.com")},
			{Email: strPtr("c@test.com")},
		},
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// GetMembersRequest field 1 = repeated MemberIds
	repeated, ok := arr[0].([]interface{})
	if !ok {
		t.Fatalf("field 1: expected array, got %T", arr[0])
	}
	if len(repeated) != 3 {
		t.Errorf("repeated field: got %d elements, want 3", len(repeated))
	}
}

// TestEncodeHighFieldNumber verifies fields with high numbers (like field 100)
// are placed in a tail object keyed by field number string.
func TestEncodeHighFieldNumber(t *testing.T) {
	msg := &pb.CreateTopicRequest{
		TextBody: strPtr("hello"),
		RequestHeader: &pb.RequestHeader{
			ClientType: pb.RequestHeader_IOS.Enum(),
		},
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// CreateTopicRequest: field 2 = text_body, field 100 = request_header
	// The array should have sequential fields up to the max sequential,
	// then a tail object for field 100.
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("not a JSON array: %v", err)
	}

	// The last element should be an object with key "100"
	lastElem := arr[len(arr)-1]
	var obj map[string]interface{}
	if err := json.Unmarshal(lastElem, &obj); err != nil {
		t.Fatalf("last element is not an object: %v (raw: %s)", err, lastElem)
	}
	if _, ok := obj["100"]; !ok {
		t.Errorf("tail object missing key '100', got keys: %v", obj)
	}
}

// TestEncodeEnumField verifies enum values encode as integers.
func TestEncodeEnumField(t *testing.T) {
	msg := &pb.RequestHeader{
		ClientType: pb.RequestHeader_IOS.Enum(),
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// RequestHeader field 2 = client_type enum (field 1 = trace_id). IOS = 2
	if arr[1] != float64(2) {
		t.Errorf("enum field: got %v, want 2 (IOS)", arr[1])
	}
}

// TestEncodeBoolField verifies bool fields encode correctly.
func TestEncodeBoolField(t *testing.T) {
	msg := &pb.CreateTopicRequest{
		TextBody:  strPtr("hi"),
		HistoryV2: boolPtr(true),
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// CreateTopicRequest field 8 = history_v2 (bool), index 7 in array
	if arr[7] != true {
		t.Errorf("bool field: got %v, want true", arr[7])
	}
}

// TestEncodeInt64Field verifies int64 fields encode as strings (JSON number safety).
func TestEncodeInt64Field(t *testing.T) {
	ts := int64(1716200000000000)
	msg := &pb.MarkGroupReadstateRequest{
		LastReadTime: &ts,
	}

	data, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}

	// Int64 values should encode as strings to avoid precision loss
	strVal, ok := arr[1].(string)
	if !ok {
		t.Fatalf("int64 field: expected string, got %T: %v", arr[1], arr[1])
	}
	if strVal != "1716200000000000" {
		t.Errorf("int64 field: got %q, want '1716200000000000'", strVal)
	}
}

// --- Decode Tests ---

// TestDecodeSimpleMessage verifies decoding a flat JSON array into a protobuf message.
func TestDecodeSimpleMessage(t *testing.T) {
	input := `["12345", 0]`
	msg := &pb.UserId{}

	if err := Decode([]byte(input), msg); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if msg.GetId() != "12345" {
		t.Errorf("field 1: got %q, want '12345'", msg.GetId())
	}
	if msg.GetType() != pb.UserType_HUMAN {
		t.Errorf("field 2: got %v, want HUMAN", msg.GetType())
	}
}

// TestDecodeNullFields verifies that null array elements leave fields unset.
func TestDecodeNullFields(t *testing.T) {
	input := `["12345", null, null, null]`
	msg := &pb.UserId{}

	if err := Decode([]byte(input), msg); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if msg.GetId() != "12345" {
		t.Errorf("field 1: got %q, want '12345'", msg.GetId())
	}
	if msg.Type != nil {
		t.Errorf("field 2: expected nil, got %v", msg.GetType())
	}
}

// TestDecodeNestedMessage verifies that nested arrays decode into nested messages.
func TestDecodeNestedMessage(t *testing.T) {
	input := `[["user1", 0]]`
	msg := &pb.MemberId{}

	if err := Decode([]byte(input), msg); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if msg.GetUserId().GetId() != "user1" {
		t.Errorf("nested field: got %q, want 'user1'", msg.GetUserId().GetId())
	}
}

// TestDecodeRepeatedField verifies that JSON arrays decode into repeated protobuf fields.
func TestDecodeRepeatedField(t *testing.T) {
	// GetMembersRequest field 1 = repeated MemberIds
	// Each MemberId has field 3 = email
	input := `[[[null, null, "a@test.com"], [null, null, "b@test.com"], [null, null, "c@test.com"]]]`
	msg := &pb.GetMembersRequest{}

	if err := Decode([]byte(input), msg); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(msg.MemberIds) != 3 {
		t.Fatalf("repeated field: got %d elements, want 3", len(msg.MemberIds))
	}
	if msg.MemberIds[0].GetEmail() != "a@test.com" {
		t.Errorf("element 0: got %q, want 'a@test.com'", msg.MemberIds[0].GetEmail())
	}
}

// TestDecodeHighFieldNumber verifies decoding when high field numbers are in a tail object.
func TestDecodeHighFieldNumber(t *testing.T) {
	// CreateTopicRequest: field 2 = text_body, field 100 = request_header
	// RequestHeader: field 1 = trace_id, field 2 = client_type. IOS = 2
	input := `[null, "hello", null, null, null, null, null, null, null, {"100": [null, 2]}]`
	msg := &pb.CreateTopicRequest{}

	if err := Decode([]byte(input), msg); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if msg.GetTextBody() != "hello" {
		t.Errorf("text_body: got %q, want 'hello'", msg.GetTextBody())
	}
	if msg.GetRequestHeader().GetClientType() != pb.RequestHeader_IOS {
		t.Errorf("request_header.client_type: got %v, want IOS", msg.GetRequestHeader().GetClientType())
	}
}

// TestDecodeInt64AsString verifies int64 fields can be decoded from string values.
func TestDecodeInt64AsString(t *testing.T) {
	// MarkGroupReadstateRequest field 2 = last_read_time (int64)
	input := `[null, "1716200000000000"]`
	msg := &pb.MarkGroupReadstateRequest{}

	if err := Decode([]byte(input), msg); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if msg.GetLastReadTime() != 1716200000000000 {
		t.Errorf("int64 field: got %d, want 1716200000000000", msg.GetLastReadTime())
	}
}

// --- Roundtrip Tests ---

// TestRoundtripSimple verifies encode→decode produces the same message.
func TestRoundtripSimple(t *testing.T) {
	id := "roundtrip_user"
	userType := pb.UserType_BOT
	original := &pb.UserId{
		Id:   &id,
		Type: &userType,
	}

	data, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded := &pb.UserId{}
	if err := Decode(data, decoded); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if !proto.Equal(original, decoded) {
		t.Errorf("roundtrip mismatch:\n  original: %v\n  decoded:  %v", original, decoded)
	}
}

// TestRoundtripComplex verifies a complex message with nested, repeated, and high fields.
func TestRoundtripComplex(t *testing.T) {
	original := &pb.CreateTopicRequest{
		TextBody: strPtr("test message"),
		LocalId:  strPtr("local_123"),
		RequestHeader: &pb.RequestHeader{
			ClientType:    pb.RequestHeader_IOS.Enum(),
			ClientVersion: int64Ptr(100),
		},
		Annotations: []*pb.Annotation{
			{Type: pb.AnnotationType_URL.Enum()},
		},
	}

	data, err := Encode(original)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded := &pb.CreateTopicRequest{}
	if err := Decode(data, decoded); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if !proto.Equal(original, decoded) {
		t.Errorf("roundtrip mismatch:\n  original: %v\n  decoded:  %v", original, decoded)
	}
}

// --- Helpers ---

// strPtr returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

// boolPtr returns a pointer to the given bool.
func boolPtr(b bool) *bool {
	return &b
}

// int64Ptr returns a pointer to the given int64.
func int64Ptr(n int64) *int64 {
	return &n
}
