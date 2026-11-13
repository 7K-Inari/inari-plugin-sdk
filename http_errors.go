package pluginsdk

import (
	"encoding/json"
	"net/http"
	"regexp"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
)

const (
	// HeaderReauth identifies the downstream provider that must re-authenticate.
	HeaderReauth = "X-Inari-Reauth"
	// ReauthCode is the machine-readable downstream re-authentication code.
	ReauthCode = "reauth_required"

	reauthDetailCode     = "code"
	reauthDetailProvider = "provider"
)

var reauthProviderPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// ReauthError constructs the typed downstream re-authentication signal. The
// fixed message deliberately excludes downstream responses and credentials.
func ReauthError(provider string) *Error {
	return &Error{
		Code:    CodeUnauthenticated,
		Message: "re-authentication required",
		Details: map[string]string{
			reauthDetailCode:     ReauthCode,
			reauthDetailProvider: provider,
		},
	}
}

// ErrorFromProto converts a wire PluginError to its SDK representation.
func ErrorFromProto(pe *pluginv1.PluginError) *Error {
	if pe == nil {
		return nil
	}
	var details map[string]string
	if pe.GetDetails() != nil {
		details = make(map[string]string, len(pe.GetDetails()))
		for key, value := range pe.GetDetails() {
			details[key] = value
		}
	}
	return &Error{Code: Code(pe.GetCode()), Message: pe.GetMessage(), Details: details}
}

// WriteHTTPError writes the canonical extension HTTP error mapping. A typed
// ReauthError is the only error that emits HeaderReauth and reauth_required.
func WriteHTTPError(w http.ResponseWriter, err error) {
	pe := ProtoError(err)
	if pe == nil {
		pe = &pluginv1.PluginError{Code: pluginv1.ErrorCode_ERROR_CODE_INTERNAL, Message: "internal error"}
	}

	status := httpStatusForPluginError(pe.GetCode())
	detail := pe.GetMessage()
	code := pe.GetCode().String()
	provider := ""
	if pe.GetCode() == pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED && pe.GetDetails()[reauthDetailCode] == ReauthCode {
		provider = pe.GetDetails()[reauthDetailProvider]
		if !reauthProviderPattern.MatchString(provider) {
			status = http.StatusInternalServerError
			detail = "invalid re-authentication provider"
			code = pluginv1.ErrorCode_ERROR_CODE_INTERNAL.String()
			provider = ""
		} else {
			status = http.StatusUnauthorized
			detail = "re-authentication required"
			code = ReauthCode
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if provider != "" {
		w.Header().Set(HeaderReauth, provider)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Detail string `json:"detail"`
		Code   string `json:"code"`
	}{Detail: detail, Code: code})
}

func httpStatusForPluginError(code pluginv1.ErrorCode) int {
	switch code {
	case pluginv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:
		return http.StatusBadRequest
	case pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED:
		return http.StatusUnauthorized
	case pluginv1.ErrorCode_ERROR_CODE_PERMISSION_DENIED:
		return http.StatusForbidden
	case pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND:
		return http.StatusNotFound
	case pluginv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION:
		return http.StatusPreconditionFailed
	case pluginv1.ErrorCode_ERROR_CODE_UNAVAILABLE:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
