// Package pblite implements Google's pblite encoding for protobuf messages.
// Pblite represents protobuf messages as JSON arrays where array indices
// correspond to protobuf field numbers. This is the wire format used by
// Google Chat's internal Dynamite protocol alongside standard binary protobuf.
package pblite

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Encode serializes a protobuf message into pblite JSON format.
// Returns a JSON array where each index maps to a protobuf field number.
func Encode(msg proto.Message) ([]byte, error) {
	val := encodeMessage(msg.ProtoReflect())
	return json.Marshal(val)
}

// encodeMessage converts a protobuf message reflection into a pblite value.
// Returns a JSON-serializable slice (sequential fields) optionally ending
// with a map for non-sequential high field numbers.
func encodeMessage(msg protoreflect.Message) interface{} {
	desc := msg.Descriptor()
	fields := desc.Fields()

	maxSeq := findMaxSequentialField(fields)
	sequential := buildSequentialArray(msg, fields, maxSeq)
	tail := buildTailObject(msg, fields, maxSeq)

	if len(tail) > 0 {
		sequential = append(sequential, tail)
	}

	return sequential
}

// findMaxSequentialField returns the highest field number that can be stored
// in the sequential portion of the array without excessive gaps.
// Fields beyond a large gap go into the tail object.
func findMaxSequentialField(fields protoreflect.FieldDescriptors) int {
	maxField := 0
	for i := 0; i < fields.Len(); i++ {
		num := int(fields.Get(i).Number())
		if num > maxField {
			maxField = num
		}
	}

	if maxField <= 20 {
		return maxField
	}

	// Find the natural break point — if there's a gap > 2x the last seen number,
	// everything after goes in the tail object.
	sorted := collectFieldNumbers(fields)
	for i := 1; i < len(sorted); i++ {
		gap := sorted[i] - sorted[i-1]
		if gap > sorted[i-1] {
			return sorted[i-1]
		}
	}

	return maxField
}

// collectFieldNumbers returns all field numbers in ascending order.
func collectFieldNumbers(fields protoreflect.FieldDescriptors) []int {
	nums := make([]int, 0, fields.Len())
	for i := 0; i < fields.Len(); i++ {
		nums = append(nums, int(fields.Get(i).Number()))
	}

	// Simple insertion sort — field count is small.
	for i := 1; i < len(nums); i++ {
		key := nums[i]
		j := i - 1
		for j >= 0 && nums[j] > key {
			nums[j+1] = nums[j]
			j--
		}
		nums[j+1] = key
	}

	return nums
}

// buildSequentialArray creates the main array portion of the pblite output.
// Indices 0..maxSeq-1 map to field numbers 1..maxSeq.
func buildSequentialArray(msg protoreflect.Message, fields protoreflect.FieldDescriptors, maxSeq int) []interface{} {
	arr := make([]interface{}, maxSeq)
	for i := 0; i < maxSeq; i++ {
		fieldNum := protoreflect.FieldNumber(i + 1)
		fd := fields.ByNumber(fieldNum)
		if fd == nil {
			arr[i] = nil
			continue
		}
		arr[i] = encodeField(msg, fd)
	}
	return arr
}

// buildTailObject creates the trailing map for high field numbers.
func buildTailObject(msg protoreflect.Message, fields protoreflect.FieldDescriptors, maxSeq int) map[string]interface{} {
	tail := make(map[string]interface{})
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		num := int(fd.Number())
		if num <= maxSeq {
			continue
		}
		val := encodeField(msg, fd)
		if val != nil {
			tail[fmt.Sprintf("%d", num)] = val
		}
	}
	return tail
}

// encodeField encodes a single protobuf field value for pblite output.
func encodeField(msg protoreflect.Message, fd protoreflect.FieldDescriptor) interface{} {
	if fd.IsList() {
		return encodeRepeatedField(msg, fd)
	}

	if !msg.Has(fd) {
		return nil
	}

	return encodeScalar(msg.Get(fd), fd)
}

// encodeRepeatedField encodes a repeated (list) protobuf field as a JSON array.
func encodeRepeatedField(msg protoreflect.Message, fd protoreflect.FieldDescriptor) interface{} {
	list := msg.Get(fd).List()
	if list.Len() == 0 {
		return nil
	}

	arr := make([]interface{}, list.Len())
	for i := 0; i < list.Len(); i++ {
		arr[i] = encodeScalar(list.Get(i), fd)
	}
	return arr
}

// encodeScalar encodes a single protobuf value based on its field descriptor kind.
func encodeScalar(val protoreflect.Value, fd protoreflect.FieldDescriptor) interface{} {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return val.Bool()

	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return float64(val.Int())

	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return float64(val.Uint())

	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return fmt.Sprintf("%d", val.Int())

	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return fmt.Sprintf("%d", val.Uint())

	case protoreflect.FloatKind:
		return float64(val.Float())

	case protoreflect.DoubleKind:
		return val.Float()

	case protoreflect.StringKind:
		return val.String()

	case protoreflect.BytesKind:
		return base64.StdEncoding.EncodeToString(val.Bytes())

	case protoreflect.EnumKind:
		return float64(val.Enum())

	case protoreflect.MessageKind, protoreflect.GroupKind:
		return encodeMessage(val.Message())

	default:
		return nil
	}
}

// int64FromFloat safely converts a float64 JSON number to int64.
// Returns an error if the value has a fractional part.
func int64FromFloat(f float64) (int64, error) {
	if f != math.Trunc(f) {
		return 0, fmt.Errorf("cannot convert %v to int64: has fractional part", f)
	}
	return int64(f), nil
}
