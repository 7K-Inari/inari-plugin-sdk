package pluginsdk

import (
	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
)

// AuthMethodType identifies one downstream authentication method a plugin
// accepts (§5.8). It mirrors pluginv1.AuthMethod_Type on the wire.
type AuthMethodType int32

const (
	AuthMethodUnspecified    AuthMethodType = AuthMethodType(pluginv1.AuthMethod_TYPE_UNSPECIFIED)
	AuthMethodOIDCUser       AuthMethodType = AuthMethodType(pluginv1.AuthMethod_TYPE_OIDC_USER)
	AuthMethodServiceAccount AuthMethodType = AuthMethodType(pluginv1.AuthMethod_TYPE_SERVICE_ACCOUNT)
	AuthMethodAPIKey         AuthMethodType = AuthMethodType(pluginv1.AuthMethod_TYPE_API_KEY)
	AuthMethodSharedSecret   AuthMethodType = AuthMethodType(pluginv1.AuthMethod_TYPE_SHARED_SECRET)
	AuthMethodOIDCSSOSession AuthMethodType = AuthMethodType(pluginv1.AuthMethod_TYPE_OIDC_SSO_SESSION)
)

var authMethodWireNames = map[AuthMethodType]string{
	AuthMethodOIDCUser:       "oidc-user",
	AuthMethodServiceAccount: "service-account",
	AuthMethodAPIKey:         "api-key",
	AuthMethodSharedSecret:   "shared-secret",
	AuthMethodOIDCSSOSession: "oidc-sso-session",
}

// String returns the wire name the control plane sends in the
// X-Inari-Auth-Method header (e.g. "oidc-user").
func (t AuthMethodType) String() string {
	if s, ok := authMethodWireNames[t]; ok {
		return s
	}
	return "unspecified"
}

// ParseAuthMethodType maps a wire name from the X-Inari-Auth-Method header to
// its AuthMethodType. ok is false for unknown names.
func ParseAuthMethodType(name string) (AuthMethodType, bool) {
	for t, s := range authMethodWireNames {
		if s == name {
			return t, true
		}
	}
	return AuthMethodUnspecified, false
}

// AuthMethod declares one way a plugin accepts downstream authentication. It
// is a declaration of capability only: the host performs any token exchange
// and injects the resulting credential separately from AuthContext.
type AuthMethod struct {
	Type AuthMethodType
	// Audience is the target audience for OIDC token exchange; only valid for
	// AuthMethodOIDCUser and AuthMethodOIDCSSOSession.
	Audience string
	// Scopes are the OAuth scopes requested during exchange; empty means
	// provider defaults. Only valid for OIDC types.
	Scopes []string
	// IsDefault marks the preferred method; at most one entry may set it.
	IsDefault bool
}

// Proto returns the wire form.
func (m AuthMethod) Proto() *pluginv1.AuthMethod {
	return &pluginv1.AuthMethod{
		Type:      pluginv1.AuthMethod_Type(m.Type),
		Audience:  m.Audience,
		Scopes:    m.Scopes,
		IsDefault: m.IsDefault,
	}
}

// AuthMethodFromProto converts the wire form to the SDK type.
func AuthMethodFromProto(pb *pluginv1.AuthMethod) AuthMethod {
	if pb == nil {
		return AuthMethod{}
	}
	return AuthMethod{
		Type:      AuthMethodType(pb.GetType()),
		Audience:  pb.GetAudience(),
		Scopes:    pb.GetScopes(),
		IsDefault: pb.GetIsDefault(),
	}
}

func (m AuthMethod) isOIDC() bool {
	return m.Type == AuthMethodOIDCUser || m.Type == AuthMethodOIDCSSOSession
}

// ValidateAuthMethods checks a declaration set: no unspecified or duplicate
// types, at most one default, and audience/scopes only on OIDC types. An
// empty set is valid — the host then applies its default (oidc-user).
func ValidateAuthMethods(methods []AuthMethod) error {
	seen := map[AuthMethodType]bool{}
	defaults := 0
	for _, m := range methods {
		if m.Type == AuthMethodUnspecified {
			return Errorf(CodeInvalidArgument, "auth method type is required")
		}
		if _, known := authMethodWireNames[m.Type]; !known {
			return Errorf(CodeInvalidArgument, "unknown auth method type %d", int32(m.Type))
		}
		if seen[m.Type] {
			return Errorf(CodeInvalidArgument, "auth method %q declared more than once", m.Type)
		}
		seen[m.Type] = true
		if m.IsDefault {
			defaults++
		}
		if !m.isOIDC() && m.Audience != "" {
			return Errorf(CodeInvalidArgument, "auth method %q does not accept an audience", m.Type)
		}
		if !m.isOIDC() && len(m.Scopes) > 0 {
			return Errorf(CodeInvalidArgument, "auth method %q does not accept scopes", m.Type)
		}
	}
	if defaults > 1 {
		return Errorf(CodeInvalidArgument, "at most one auth method may be the default")
	}
	return nil
}
