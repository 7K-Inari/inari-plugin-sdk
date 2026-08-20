// Package pluginsdk is the Inari SDK for backend extensions (platform plan
// §5.8). Plugins run as sidecar processes over hashicorp go-plugin and serve
// the versioned inari.plugin.v1 gRPC contract from inari-api.
package pluginsdk

import (
	"errors"
	"fmt"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
)

// Code classifies plugin errors; it mirrors pluginv1.ErrorCode on the wire.
type Code int32

const (
	CodeInvalidArgument    Code = Code(pluginv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT)
	CodeUnauthenticated    Code = Code(pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED)
	CodePermissionDenied   Code = Code(pluginv1.ErrorCode_ERROR_CODE_PERMISSION_DENIED)
	CodeNotFound           Code = Code(pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND)
	CodeFailedPrecondition Code = Code(pluginv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION)
	CodeInternal           Code = Code(pluginv1.ErrorCode_ERROR_CODE_INTERNAL)
	CodeUnavailable        Code = Code(pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE)
)

// Error is a structured plugin error carried in the invoke envelope.
type Error struct {
	Code    Code
	Message string
	Details map[string]string
}

func (e *Error) Error() string { return e.Message }

// WithDetail attaches a machine-readable detail pair and returns the error.
func (e *Error) WithDetail(key, value string) *Error {
	if e.Details == nil {
		e.Details = map[string]string{}
	}
	e.Details[key] = value
	return e
}

// Errorf returns a new *Error with the given code and formatted message.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// ProtoError converts any handler error to the wire form. Errors not created
// via Errorf map to CodeInternal. Nil maps to nil.
func ProtoError(err error) *pluginv1.PluginError {
	if err == nil {
		return nil
	}
	var pe *Error
	if errors.As(err, &pe) {
		return &pluginv1.PluginError{
			Code:    pluginv1.ErrorCode(pe.Code),
			Message: pe.Message,
			Details: pe.Details,
		}
	}
	return &pluginv1.PluginError{
		Code:    pluginv1.ErrorCode_ERROR_CODE_INTERNAL,
		Message: err.Error(),
	}
}
