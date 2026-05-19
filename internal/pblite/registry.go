// registry.go provides protobuf message type resolution for dynamic message creation.
// Used by the decoder to instantiate nested message types by their full name.
package pblite

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// protoregistry_findMessageType looks up a registered protobuf message type by name.
// All generated Go protobuf types self-register, so this works for any imported proto package.
func protoregistry_findMessageType(name protoreflect.FullName) (protoreflect.MessageType, error) {
	return protoregistry.GlobalTypes.FindMessageByName(name)
}
