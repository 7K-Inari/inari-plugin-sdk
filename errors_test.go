package pluginsdk_test

import (
	"errors"
	"testing"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func TestErrorfMapsToProtoError(t *testing.T) {
	err := pluginsdk.Errorf(pluginsdk.CodeNotFound, "action %q not found", "deploy")
	pe := pluginsdk.ProtoError(err)
	if pe.GetCode() != pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND {
		t.Fatalf("code = %v, want NOT_FOUND", pe.GetCode())
	}
	if pe.GetMessage() != `action "deploy" not found` {
		t.Fatalf("message = %q", pe.GetMessage())
	}
}

func TestUnknownErrorMapsToInternal(t *testing.T) {
	pe := pluginsdk.ProtoError(errors.New("boom"))
	if pe.GetCode() != pluginv1.ErrorCode_ERROR_CODE_INTERNAL {
		t.Fatalf("code = %v, want INTERNAL", pe.GetCode())
	}
	if pe.GetMessage() != "boom" {
		t.Fatalf("message = %q", pe.GetMessage())
	}
}

func TestErrorDetails(t *testing.T) {
	err := pluginsdk.Errorf(pluginsdk.CodeInvalidArgument, "bad field").WithDetail("field", "name")
	pe := pluginsdk.ProtoError(err)
	if pe.GetDetails()["field"] != "name" {
		t.Fatalf("details = %v", pe.GetDetails())
	}
}

func TestNilErrorMapsToNil(t *testing.T) {
	if pluginsdk.ProtoError(nil) != nil {
		t.Fatal("expected nil")
	}
}
