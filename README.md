# inari-plugin-sdk

Go SDK for Inari backend extensions: handshake, lifecycle, auth context, helpers (plan §6 #6, §5.8).

Plugins run as sidecar processes next to the `inari-server` Extension Host using the hashicorp go-plugin model: magic-cookie handshake, protocol version negotiation, SHA-256 checksum verification, and crash isolation. The control plane authenticates callers at `/api/extensions/<name>/*`, enforces RBAC, and forwards the authenticated principal + tenant to the plugin on every call — plugins never see raw credentials.

Stack: Go, hashicorp go-plugin. Contract: `inari.plugin.v1` protobuf from [`inari-api`](https://github.com/7K-Inari/inari-api), pinned as a Go module.

## Quickstart

```go
package main

import (
	"context"
	"fmt"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func main() {
	p := pluginsdk.New(pluginsdk.Info{Name: "my-plugin", Version: "0.1.0"})

	_ = p.RegisterAction(pluginsdk.Action{
		Name:        "greet",
		Description: "Greets the authenticated caller.",
		Handler: func(ctx context.Context, req *pluginsdk.Request) (*pluginsdk.Response, error) {
			ac, _ := pluginsdk.AuthContextFrom(ctx) // guaranteed valid: SDK fails closed
			return &pluginsdk.Response{
				Result: []byte(fmt.Sprintf(`{"message":"hello %s of %s"}`, ac.PrincipalID, ac.TenantID)),
			}, nil
		},
	})

	_ = p.Serve(context.Background()) // handshake, gRPC serving, lifecycle, signal handling
}
```

## Testing with the testkit

```go
func TestMyPlugin(t *testing.T) {
	client := testkit.Run(t, newPlugin())
	resp, err := client.Invoke(ctx, "greet", pluginsdk.AuthContext{
		PrincipalID: "user-1", TenantID: "org:acme",
	}, nil)
	// assert on resp.Result / err (*pluginsdk.Error)
}
```

See `testkit/example_test.go` for a complete third-party-style test and `host/` for the reference subprocess launcher (handshake, checksum pinning, crash detection) used by the integration tests.

## Lifecycle

- `pluginsdk.WithHooks(...)` — `OnInit` before serving, `OnShutdown` on SIGTERM/SIGINT.
- `pluginsdk.WithHealthChecker(...)` — backs the `HealthCheck` RPC the host uses for supervision.
- `pluginsdk.NewLogger(name)` — `slog` bridged into the host's structured log stream.

## Error semantics

Return `pluginsdk.Errorf(code, ...)` from handlers; codes map to the contract's `PluginError` (`INVALID_ARGUMENT`, `UNAUTHENTICATED`, `PERMISSION_DENIED`, `NOT_FOUND`, `FAILED_PRECONDITION`, `INTERNAL`, `UNAVAILABLE`). Unknown errors map to `INTERNAL`. Auth context validation fails closed with `UNAUTHENTICATED` before your handler runs.

## Versioning

Conventional Commits; releases via release-please (merge the Release PR → tag `vX.Y.Z` → `go get github.com/7K-Inari/inari-plugin-sdk@vX.Y.Z`). The `inari-api` contract module is pinned in `go.mod`; upgrade it deliberately.

Authoring guide (draft for inari-docs): [`docs/authoring-guide.md`](docs/authoring-guide.md).
