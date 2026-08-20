package pluginsdk

import (
	"context"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
)

// AuthContext is the authenticated principal and tenant identity forwarded by
// the control plane for every Invoke call (§5.8). Plugins never receive raw
// credentials — the control plane authenticates at /api/extensions/<name>/*,
// enforces RBAC, and forwards this identity.
type AuthContext struct {
	PrincipalID string
	TenantID    string
	DisplayName string
	Groups      []string
	Claims      map[string]string
}

// Validate enforces the fail-closed identity contract: both principal and
// tenant must be present.
func (a AuthContext) Validate() error {
	if a.PrincipalID == "" {
		return Errorf(CodeUnauthenticated, "missing principal id in auth context")
	}
	if a.TenantID == "" {
		return Errorf(CodeUnauthenticated, "missing tenant id in auth context")
	}
	return nil
}

// AuthContextFromProto converts the wire form to the SDK type.
func AuthContextFromProto(pb *pluginv1.AuthContext) AuthContext {
	if pb == nil {
		return AuthContext{}
	}
	return AuthContext{
		PrincipalID: pb.GetPrincipalId(),
		TenantID:    pb.GetTenantId(),
		DisplayName: pb.GetDisplayName(),
		Groups:      pb.GetGroups(),
		Claims:      pb.GetClaims(),
	}
}

// Proto returns the wire form.
func (a AuthContext) Proto() *pluginv1.AuthContext {
	return &pluginv1.AuthContext{
		PrincipalId: a.PrincipalID,
		TenantId:    a.TenantID,
		DisplayName: a.DisplayName,
		Groups:      a.Groups,
		Claims:      a.Claims,
	}
}

type authContextKey struct{}

// ContextWithAuth attaches an AuthContext to ctx.
func ContextWithAuth(ctx context.Context, ac AuthContext) context.Context {
	return context.WithValue(ctx, authContextKey{}, ac)
}

// AuthContextFrom extracts the AuthContext attached to ctx. Handlers use it to
// access the authenticated principal and tenant of the current call.
func AuthContextFrom(ctx context.Context) (AuthContext, bool) {
	ac, ok := ctx.Value(authContextKey{}).(AuthContext)
	return ac, ok
}
