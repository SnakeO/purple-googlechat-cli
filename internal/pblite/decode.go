// decode.go implements pblite JSON array → protobuf message decoding.
// It walks the JSON array using protobuf reflection to populate message fields
// based on array index → field number mapping.
package pblite

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Decode deserializes a pblite JSON array into a protobuf message.
// The target message must be pre-allocated (e.g., &pb.UserId{}).
func Decode(data []byte, msg proto.Message) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("pblite: invalid JSON array: %w", err)
	}

	return decodeMessage(raw, msg.ProtoReflect())
}

// decodeMessage populates a protobuf message from a pblite JSON array.
func decodeMessage(arr []json.RawMessage, msg protoreflect.Message) error {
	fields := msg.Descriptor().Fields()

	// Google's pblite responses can have a method-name string as the first
	// element (e.g., "dfe.w.pw"). Skip it if present.
	startIdx := 0
	if len(arr) > 0 && isMethodName(arr[0]) {
		startIdx = 1
	}

	for i, elem := range arr[startIdx:] {
		if isNull(elem) {
			continue
		}

		if isTailObject(elem) {
			if err := decodeTailObject(elem, msg, fields); err != nil {
				return err
			}
			continue
		}

		fieldNum := protoreflect.FieldNumber(i + 1)
		fd := fields.ByNumber(fieldNum)
		if fd == nil {
			continue
		}

		if err := decodeField(elem, msg, fd); err != nil {
			return fmt.Errorf("field %d: %w", fieldNum, err)
		}
	}

	return nil
}

// isNull returns true if the JSON element is a literal null.
func isNull(data json.RawMessage) bool {
	return len(data) == 4 && string(data) == "null"
}

// isMethodName returns true if the element is a Google pblite method name string
// (e.g., "dfe.w.pw", "dfe.ust.gsus"). These are prefixed to pblite arrays.
func isMethodName(data json.RawMessage) bool {
	var s string
	if json.Unmarshal(data, &s) != nil {
		return false
	}
	return strings.HasPrefix(s, "dfe.")
}

// isTailObject returns true if the JSON element is an object (starts with '{').
func isTailObject(data json.RawMessage) bool {
	for _, b := range data {
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' {
			continue
		}
		return b == '{'
	}
	return false
}

// decodeTailObject handles the trailing map of high field numbers.
func decodeTailObject(data json.RawMessage, msg protoreflect.Message, fields protoreflect.FieldDescriptors) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("pblite: invalid tail object: %w", err)
	}

	for key, val := range obj {
		num, err := strconv.Atoi(key)
		if err != nil {
			continue
		}

		fd := fields.ByNumber(protoreflect.FieldNumber(num))
		if fd == nil {
			continue
		}

		if err := decodeField(val, msg, fd); err != nil {
			return fmt.Errorf("tail field %d: %w", num, err)
		}
	}

	return nil
}

// decodeField decodes a single JSON value into a protobuf field.
func decodeField(data json.RawMessage, msg protoreflect.Message, fd protoreflect.FieldDescriptor) error {
	if fd.IsList() {
		return decodeRepeatedField(data, msg, fd)
	}

	val, err := decodeScalar(data, fd)
	if err != nil {
		return err
	}

	msg.Set(fd, val)
	return nil
}

// decodeRepeatedField decodes a JSON array into a repeated protobuf field.
func decodeRepeatedField(data json.RawMessage, msg protoreflect.Message, fd protoreflect.FieldDescriptor) error {
	var elems []json.RawMessage
	if err := json.Unmarshal(data, &elems); err != nil {
		return fmt.Errorf("expected array for repeated field: %w", err)
	}

	list := msg.Mutable(fd).List()
	for _, elem := range elems {
		if isNull(elem) {
			continue
		}

		val, err := decodeScalar(elem, fd)
		if err != nil {
			return err
		}
		list.Append(val)
	}

	return nil
}

// decodeScalar decodes a single JSON value into a protoreflect.Value.
func decodeScalar(data json.RawMessage, fd protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return decodeBool(data)

	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return decodeInt32(data)

	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return decodeUint32(data)

	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return decodeInt64(data)

	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return decodeUint64(data)

	case protoreflect.FloatKind:
		return decodeFloat32(data)

	case protoreflect.DoubleKind:
		return decodeFloat64(data)

	case protoreflect.StringKind:
		return decodeString(data)

	case protoreflect.BytesKind:
		return decodeBytes(data)

	case protoreflect.EnumKind:
		return decodeEnum(data)

	case protoreflect.MessageKind, protoreflect.GroupKind:
		return decodeNestedMessage(data, fd)

	default:
		return protoreflect.Value{}, fmt.Errorf("unsupported field kind: %v", fd.Kind())
	}
}

// --- Scalar decoders ---

// decodeBool decodes a JSON boolean value.
func decodeBool(data json.RawMessage) (protoreflect.Value, error) {
	var v bool
	if err := json.Unmarshal(data, &v); err != nil {
		return protoreflect.Value{}, fmt.Errorf("expected bool: %w", err)
	}
	return protoreflect.ValueOfBool(v), nil
}

// decodeInt32 decodes a JSON number or string into an int32 value.
func decodeInt32(data json.RawMessage) (protoreflect.Value, error) {
	n, err := decodeNumber(data)
	if err != nil {
		return protoreflect.Value{}, err
	}
	return protoreflect.ValueOfInt32(int32(n)), nil
}

// decodeUint32 decodes a JSON number or string into a uint32 value.
func decodeUint32(data json.RawMessage) (protoreflect.Value, error) {
	n, err := decodeNumber(data)
	if err != nil {
		return protoreflect.Value{}, err
	}
	return protoreflect.ValueOfUint32(uint32(n)), nil
}

// decodeInt64 decodes a JSON number or string into an int64 value.
// Pblite encodes int64 as strings to avoid JavaScript precision loss.
func decodeInt64(data json.RawMessage) (protoreflect.Value, error) {
	n, err := decodeNumber(data)
	if err != nil {
		return protoreflect.Value{}, err
	}
	return protoreflect.ValueOfInt64(n), nil
}

// decodeUint64 decodes a JSON number or string into a uint64 value.
func decodeUint64(data json.RawMessage) (protoreflect.Value, error) {
	n, err := decodeNumber(data)
	if err != nil {
		return protoreflect.Value{}, err
	}
	return protoreflect.ValueOfUint64(uint64(n)), nil
}

// decodeFloat32 decodes a JSON number into a float32 value.
func decodeFloat32(data json.RawMessage) (protoreflect.Value, error) {
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return protoreflect.Value{}, fmt.Errorf("expected number: %w", err)
	}
	return protoreflect.ValueOfFloat32(float32(v)), nil
}

// decodeFloat64 decodes a JSON number into a float64 value.
func decodeFloat64(data json.RawMessage) (protoreflect.Value, error) {
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return protoreflect.Value{}, fmt.Errorf("expected number: %w", err)
	}
	return protoreflect.ValueOfFloat64(v), nil
}

// decodeString decodes a JSON string value.
func decodeString(data json.RawMessage) (protoreflect.Value, error) {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return protoreflect.Value{}, fmt.Errorf("expected string: %w", err)
	}
	return protoreflect.ValueOfString(v), nil
}

// decodeBytes decodes a base64-encoded JSON string into bytes.
func decodeBytes(data json.RawMessage) (protoreflect.Value, error) {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return protoreflect.Value{}, fmt.Errorf("expected string: %w", err)
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return protoreflect.Value{}, fmt.Errorf("invalid base64: %w", err)
	}
	return protoreflect.ValueOfBytes(b), nil
}

// decodeEnum decodes a JSON number into an enum value.
func decodeEnum(data json.RawMessage) (protoreflect.Value, error) {
	n, err := decodeNumber(data)
	if err != nil {
		return protoreflect.Value{}, err
	}
	return protoreflect.ValueOfEnum(protoreflect.EnumNumber(n)), nil
}

// decodeNestedMessage decodes a JSON array into a nested protobuf message.
// If the data is a scalar (string/number) instead of an array, it's treated
// as field 1 of the nested message — a server-side shorthand.
func decodeNestedMessage(data json.RawMessage, fd protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		// Not an array — treat as field 1 shorthand
		return decodeScalarAsMessage(data, fd)
	}

	newMsg := dynamicNew(fd.Message())
	if err := decodeMessage(arr, newMsg); err != nil {
		return protoreflect.Value{}, err
	}

	return protoreflect.ValueOfMessage(newMsg), nil
}

// decodeScalarAsMessage handles the case where a scalar value represents a message
// with only field 1 set. Common in Google's pblite responses.
func decodeScalarAsMessage(data json.RawMessage, fd protoreflect.FieldDescriptor) (protoreflect.Value, error) {
	newMsg := dynamicNew(fd.Message())
	field1 := fd.Message().Fields().ByNumber(1)
	if field1 == nil {
		return protoreflect.Value{}, fmt.Errorf("message %s has no field 1 for scalar shorthand", fd.Message().FullName())
	}

	val, err := decodeScalar(data, field1)
	if err != nil {
		return protoreflect.Value{}, fmt.Errorf("scalar shorthand for %s: %w", fd.Message().FullName(), err)
	}

	newMsg.Set(field1, val)
	return protoreflect.ValueOfMessage(newMsg), nil
}

// dynamicNew creates a new mutable message instance from a descriptor.
func dynamicNew(desc protoreflect.MessageDescriptor) protoreflect.Message {
	mt, err := protoregistry_findMessageType(desc.FullName())
	if err != nil {
		panic(fmt.Sprintf("pblite: message type not found: %s", desc.FullName()))
	}
	return mt.New()
}

// decodeNumber handles both JSON number and string representations of integers.
// Google's pblite format encodes int64 as strings to avoid precision loss.
func decodeNumber(data json.RawMessage) (int64, error) {
	// Try string first (int64 values are often stringified)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		return strconv.ParseInt(s, 10, 64)
	}

	// Fall back to JSON number
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return 0, fmt.Errorf("expected number or string: %s", data)
	}

	if f != math.Trunc(f) {
		return 0, fmt.Errorf("cannot convert %v to integer", f)
	}

	return int64(f), nil
}
