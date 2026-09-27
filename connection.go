package pluginsdk

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc/metadata"
)

// Host-injected metadata keys carrying downstream credentials and connection
// configuration for the current Invoke call (§5.8). HTTP headers such as
// X-Inari-Downstream-Authorization arrive plugin-side lowercased as gRPC
// metadata. Values under these keys are credentials: never log them.
const (
	// HeaderAuthMethod names the metadata key holding the wire name of the
	// auth method the host used (e.g. "oidc-user").
	HeaderAuthMethod = "x-inari-auth-method"
	// HeaderDownstreamAuthorization names the metadata key holding the
	// downstream credential (typically "Bearer <token>").
	HeaderDownstreamAuthorization = "x-inari-downstream-authorization"
	// headerConnectionConfigPrefix prefixes non-sensitive connection config
	// entries: x-inari-connection-config-<key>.
	headerConnectionConfigPrefix = "x-inari-connection-config-"
)

// Connection carries the host-injected downstream credential and connection
// configuration for a single Invoke call. It is transport metadata, kept
// strictly separate from AuthContext (which is identity-only). The token is
// never exposed via String or logs.
type Connection struct {
	// AuthMethod is the method the host used to obtain the credential, or
	// AuthMethodUnspecified when the header is absent or names an unknown
	// method.
	AuthMethod AuthMethodType
	// methodSet records that the host sent an auth-method header at all; an
	// unparseable header is fail-closed, an absent header falls back to the
	// plugin default.
	methodSet bool
	token     string
	config    map[string]string
}

// String returns a redacted, log-safe description of the connection.
func (c Connection) String() string {
	tokenState := "absent"
	if c.token != "" {
		tokenState = "present(<redacted>)"
	}
	return fmt.Sprintf("Connection{method:%s token:%s}", c.AuthMethod, tokenState)
}

// connectionFromMetadata parses host-injected connection metadata. Parsing is
// fail-open at this layer (unknown method names map to AuthMethodUnspecified);
// enforcement against the plugin's declared methods happens in Invoke.
func connectionFromMetadata(md metadata.MD) Connection {
	c := Connection{}
	if v := md.Get(HeaderAuthMethod); len(v) > 0 {
		c.methodSet = true
		if t, ok := ParseAuthMethodType(v[0]); ok {
			c.AuthMethod = t
		}
	}
	if v := md.Get(HeaderDownstreamAuthorization); len(v) > 0 {
		c.token = v[0]
	}
	for k, vals := range md {
		if !strings.HasPrefix(k, headerConnectionConfigPrefix) || len(vals) == 0 {
			continue
		}
		if c.config == nil {
			c.config = map[string]string{}
		}
		c.config[strings.TrimPrefix(k, headerConnectionConfigPrefix)] = vals[0]
	}
	return c
}

// resolveAgainst validates the connection against the plugin's declared auth
// methods. Fail-closed: an unsupported or unspecified method and a missing
// credential are errors. Returns the effective method (falling back to the
// declared default, else the first declared method).
func (c Connection) resolveAgainst(declared []AuthMethod) (AuthMethodType, error) {
	method := c.AuthMethod
	if c.methodSet && method == AuthMethodUnspecified {
		return AuthMethodUnspecified, Errorf(CodeFailedPrecondition,
			"unknown auth method header value")
	}
	if method == AuthMethodUnspecified {
		for _, m := range declared {
			if m.IsDefault {
				method = m.Type
				break
			}
		}
		if method == AuthMethodUnspecified {
			method = declared[0].Type
		}
	} else {
		supported := false
		for _, m := range declared {
			if m.Type == method {
				supported = true
				break
			}
		}
		if !supported {
			return AuthMethodUnspecified, Errorf(CodeFailedPrecondition,
				"auth method %q is not declared by this plugin", method)
		}
	}
	if c.token == "" {
		return AuthMethodUnspecified, Errorf(CodeUnauthenticated,
			"missing downstream credential for auth method %q", method)
	}
	return method, nil
}

type connectionKey struct{}

// ContextWithConnection attaches a Connection to ctx.
func ContextWithConnection(ctx context.Context, c Connection) context.Context {
	return context.WithValue(ctx, connectionKey{}, c)
}

// ConnectionFrom extracts the Connection attached to ctx.
func ConnectionFrom(ctx context.Context) (Connection, bool) {
	c, ok := ctx.Value(connectionKey{}).(Connection)
	return c, ok
}

// DownstreamToken returns the host-injected downstream credential (verbatim,
// e.g. "Bearer <token>") for the current call. ok is false when no credential
// was injected. Never log the returned value.
func DownstreamToken(ctx context.Context) (string, bool) {
	c, ok := ConnectionFrom(ctx)
	if !ok || c.token == "" {
		return "", false
	}
	return c.token, true
}

// ConnectionAuthMethod returns the auth method the host used for the current
// call. ok is false when no connection is attached or the method is
// unspecified.
func ConnectionAuthMethod(ctx context.Context) (AuthMethodType, bool) {
	c, ok := ConnectionFrom(ctx)
	if !ok || c.AuthMethod == AuthMethodUnspecified {
		return AuthMethodUnspecified, false
	}
	return c.AuthMethod, true
}

// ConnectionConfig returns one non-sensitive host-injected connection config
// value by key.
func ConnectionConfig(ctx context.Context, key string) (string, bool) {
	c, ok := ConnectionFrom(ctx)
	if !ok {
		return "", false
	}
	v, ok := c.config[key]
	return v, ok
}

// ConnectionConfigs returns a copy of all non-sensitive host-injected
// connection config values.
func ConnectionConfigs(ctx context.Context) map[string]string {
	c, ok := ConnectionFrom(ctx)
	if !ok || len(c.config) == 0 {
		return nil
	}
	out := make(map[string]string, len(c.config))
	for k, v := range c.config {
		out[k] = v
	}
	return out
}
