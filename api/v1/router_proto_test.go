package apiv1

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestCommitEntryConfigTextFieldIsDeprecated(t *testing.T) {
	fields := File_api_v1_router_proto.Messages().ByName(protoreflect.Name("CommitEntry")).Fields()
	field := fields.ByName(protoreflect.Name("config_text"))
	if field == nil {
		t.Fatal("CommitEntry.config_text descriptor not found")
	}
	options, ok := field.Options().(*descriptorpb.FieldOptions)
	if !ok {
		t.Fatalf("CommitEntry.config_text options = %T, want *descriptorpb.FieldOptions", field.Options())
	}
	if !options.GetDeprecated() {
		t.Fatal("CommitEntry.config_text deprecated option = false, want true")
	}
}
