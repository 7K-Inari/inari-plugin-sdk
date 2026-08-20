# Authoring a Backend Extension for Inari (draft)

> Draft for the Inari extension-author guide (to be moved to `inari-docs`).
> Audience: engineers building backend extensions against `inari-plugin-sdk`.

## What a backend extension is

A backend extension ("plugin") is a small Go binary that runs as a **sidecar process** next to the Inari control plane's Extension Host (platform plan §5.8). It speaks the versioned `inari.plugin.v1` gRPC contract from `inari-api` over the hashicorp go-plugin transport:

- **Handshake** — a magic cookie and protocol version guard startup; the host refuses anything that doesn't present both.
- **Checksum verification** — the host pins the SHA-256 of the plugin binary; a tampered binary never starts.
- **Crash isolation** — a plugin that panics or exits is detected by the host supervisor and its calls fail fast with a typed crash error; the control plane itself is unaffected.

Plugin functionality surfaces to users through **`/api/extensions/<name>/*`**: the control plane authenticates the caller (OIDC JWT), enforces RBAC (`extensions, invoke, <name>`), strips sensitive headers, and forwards the call to your plugin **with an authenticated identity** — you never handle credentials.

## The 5-minute plugin

```go
package main

import (
	"context"
	"fmt"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func main() {
	p := pluginsdk.New(pluginsdk.Info{Name: "my-plugin", Version: "0.1.0"})

	err := p.RegisterAction(pluginsdk.Action{
		Name:        "greet",
		Description: "Greets the authenticated caller within their tenant.",
		Handler: func(ctx context.Context, req *pluginsdk.Request) (*pluginsdk.Response, error) {
			ac, _ := pluginsdk.AuthContextFrom(ctx)
			return &pluginsdk.Response{
				Result: []byte(fmt.Sprintf(`{"message":"hello %s of %s"}`, ac.PrincipalID, ac.TenantID)),
			}, nil
		},
	})
	if err != nil {
		panic(err)
	}

	if err := p.Serve(context.Background()); err != nil {
		panic(err)
	}
}
```

`Serve` handles the handshake, protocol negotiation, gRPC serving, lifecycle hooks, and graceful shutdown. Your `main` stays this small.

## Identity and tenancy: the auth context

Every `Invoke` carries an `AuthContext` — the **authenticated principal** and **tenant** the control plane already verified:

```go
ac, ok := pluginsdk.AuthContextFrom(ctx)
ac.PrincipalID // stable subject id, e.g. "user-42"
ac.TenantID    // Keycloak Organization, e.g. "org:acme"
ac.Groups      // e.g. ["tenant-acme/platform-team"]
ac.Claims      // additional verified claims the control plane forwarded
```

The SDK **fails closed**: calls without a principal or tenant never reach your handler (`UNAUTHENTICATED`). Rules of thumb:

- **Always scope reads and writes by `ac.TenantID`.** Inari is tenant-aware to the core; an action that ignores tenancy is a cross-tenant leak.
- Treat `AuthContext` as your only source of identity — do not accept user/tenant ids from the payload.
- Include `ac.PrincipalID` in your own audit/log lines; the control plane correlates by `RequestID`.

## Declaring capabilities

Each action is declared with a name, description, and optional JSON Schemas:

```go
p.RegisterAction(pluginsdk.Action{
	Name:         "resize",
	Description:  "Resizes a resource.",
	InputSchema:  inputSchema,  // JSON Schema, validated/rendered by the control plane
	OutputSchema: outputSchema,
	Handler:      resize,
})
```

`GetCapabilities` serves this list to the host. Providing schemas lets the console render forms and the control plane pre-validate payloads.

## Errors

Return structured errors with `pluginsdk.Errorf`:

```go
return nil, pluginsdk.Errorf(pluginsdk.CodeNotFound, "resource %q not found", id).
	WithDetail("resource", id)
```

Codes (`INVALID_ARGUMENT`, `UNAUTHENTICATED`, `PERMISSION_DENIED`, `NOT_FOUND`, `FAILED_PRECONDITION`, `INTERNAL`, `UNAVAILABLE`) travel on the wire as `PluginError`; anything else maps to `INTERNAL`. Use specific codes — the control plane renders them into actionable user feedback.

## Lifecycle, health, and logging

```go
p := pluginsdk.New(info,
	pluginsdk.WithHooks(myHooks{}),           // OnInit before serving; OnShutdown on SIGTERM
	pluginsdk.WithHealthChecker(myHealth{}),  // backs the host's supervision
)
log := pluginsdk.NewLogger("my-plugin")     // slog bridged into the host's log stream
```

Use `OnInit` for warm-up (dial dependencies, load config) and `OnShutdown` to flush/close. Implement `Health(ctx) (bool, string)` to report degraded dependencies — the host uses it to route or restart.

## Testing with the testkit

You never need a subprocess to test your plugin:

```go
func TestResize(t *testing.T) {
	client := testkit.Run(t, newPlugin())

	resp, err := client.Invoke(ctx, "resize",
		pluginsdk.AuthContext{PrincipalID: "user-1", TenantID: "org:acme"},
		[]byte(`{"replicas": 3}`),
	)
	if err != nil { t.Fatal(err) }
	// assert on resp.Result

	// Fail-closed behavior is testable too:
	_, err = client.Invoke(ctx, "resize", pluginsdk.AuthContext{}, nil)
	// err is *pluginsdk.Error with CodeUnauthenticated
}
```

`testkit.Run` serves your plugin in-process over the real gRPC contract, so handler, auth-context, and error-mapping behavior are all exercised. See `testkit/example_test.go` in the SDK repo for a complete example; `examples/greeter` plus `host/host_test.go` show the end-to-end subprocess path (handshake, checksum, crash isolation).

## Shipping

1. Conventional Commits; release-please tags `vX.Y.Z` for you.
2. Keep your dependency on `github.com/7K-Inari/inari-api` pinned and upgrade deliberately — that module **is** the compatibility contract.
3. The host pins your binary's SHA-256; publish checksums (and cosign signatures where available) with releases.
