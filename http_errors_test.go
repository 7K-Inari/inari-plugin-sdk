package pluginsdk_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func TestReauthErrorMapsToProto(t *testing.T) {
	err := pluginsdk.ReauthError("argocd")
	pe := pluginsdk.ProtoError(err)
	if pe.GetCode() != pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
		t.Fatalf("code = %v, want UNAUTHENTICATED", pe.GetCode())
	}
	if pe.GetMessage() != "re-authentication required" {
		t.Fatalf("message = %q", pe.GetMessage())
	}
	if pe.GetDetails()["code"] != pluginsdk.ReauthCode || pe.GetDetails()["provider"] != "argocd" {
		t.Fatalf("details = %+v", pe.GetDetails())
	}
}

func TestErrorFromProto(t *testing.T) {
	if got := pluginsdk.ErrorFromProto(nil); got != nil {
		t.Fatalf("nil conversion = %+v", got)
	}
	got := pluginsdk.ErrorFromProto(&pluginv1.PluginError{
		Code:    pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND,
		Message: "missing",
		Details: map[string]string{"resource": "app"},
	})
	if got.Code != pluginsdk.CodeNotFound || got.Message != "missing" || got.Details["resource"] != "app" {
		t.Fatalf("conversion = %+v", got)
	}
}

func TestWriteHTTPReauthError(t *testing.T) {
	rec := httptest.NewRecorder()
	pluginsdk.WriteHTTPError(rec, pluginsdk.ReauthError("argocd"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get(pluginsdk.HeaderReauth); got != "argocd" {
		t.Fatalf("%s = %q", pluginsdk.HeaderReauth, got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["detail"] != "re-authentication required" || body["code"] != pluginsdk.ReauthCode {
		t.Fatalf("body = %+v", body)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("body leaks credential material: %s", rec.Body.String())
	}
}

func TestWriteHTTPErrorStatusMapping(t *testing.T) {
	tests := []struct {
		code pluginsdk.Code
		want int
	}{
		{pluginsdk.CodeInvalidArgument, 400},
		{pluginsdk.CodeUnauthenticated, 401},
		{pluginsdk.CodePermissionDenied, 403},
		{pluginsdk.CodeNotFound, 404},
		{pluginsdk.CodeFailedPrecondition, 412},
		{pluginsdk.CodeUnavailable, 503},
		{pluginsdk.CodeInternal, 500},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			rec := httptest.NewRecorder()
			pluginsdk.WriteHTTPError(rec, fmt.Errorf("wrapped: %w", pluginsdk.Errorf(tc.code, "safe message")))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if rec.Header().Get(pluginsdk.HeaderReauth) != "" {
				t.Fatal("non-reauth error set provider header")
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["detail"] != "safe message" || body["code"] == "" {
				t.Fatalf("body = %+v", body)
			}
		})
	}
}

func TestWriteHTTPErrorUnknownAndInvalidProvider(t *testing.T) {
	for name, err := range map[string]error{
		"unknown":         errors.New("boom"),
		"empty provider":  pluginsdk.ReauthError(""),
		"unsafe provider": pluginsdk.ReauthError("argocd\r\nX-Evil: yes"),
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			pluginsdk.WriteHTTPError(rec, err)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}
			if rec.Header().Get(pluginsdk.HeaderReauth) != "" {
				t.Fatal("invalid error emitted a reauth provider")
			}
		})
	}
}
