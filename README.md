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

## Imperative tenant operations

Use the typed `host/agentgateway` client when an action must run through the tenant agent. The client owns the versioned Connect contract, per-extension authentication, tenant/cluster metadata, and downstream-credential forwarding; extensions never dial a private gateway or copy its proto.

```go
import (
  "github.com/7K-Inari/inari-plugin-sdk/host/agentgateway"
  "golang.org/x/oauth2/clientcredentials"
)

tokens := (&clientcredentials.Config{
  ClientID: extensionClientID,
  ClientSecret: extensionClientSecret,
  TokenURL: issuerTokenURL,
}).TokenSource(ctx)
gateway, err := agentgateway.NewClient(agentgateway.Config{
  BaseURL: controlPlaneURL,
  TokenSource: tokens,
})
```

Inside a registered action, pass the invocation `ctx`, a validated cluster ID, and an `agentv1.InvokeAction`. The SDK derives the tenant from `AuthContext` and any downstream credential from the current connection. It rejects self-asserted credential references, never mutates the caller's command, and does not retry imperative operations. A disconnected agent fails closed with `CodeUnavailable`.

```go
ack, err := gateway.InvokeAction(ctx, clusterID, &agentv1.InvokeAction{Action: "sync"})
```

See [`inari-api/docs/extension-gateway.md`](https://github.com/7K-Inari/inari-api/blob/main/docs/extension-gateway.md) for routing, security, and error semantics.

## Re-authentication and HTTP errors

Return `pluginsdk.ReauthError("argocd")` when the downstream session is missing or expired. It produces `UNAUTHENTICATED` with `details{code: reauth_required, provider: argocd}` and a fixed message that cannot leak provider responses. HTTP adapters use the shared mapper instead of hand-written status, header, or JSON logic:

```go
if pe := resp.GetError(); pe != nil {
  pluginsdk.WriteHTTPError(w, pluginsdk.ErrorFromProto(pe))
  return
}
```

For a valid provider this emits `401`, `X-Inari-Reauth: argocd`, and `{"detail":"re-authentication required","code":"reauth_required"}`. Invalid provider configuration fails closed with `500`; other SDK error codes use the same canonical HTTP status table.

To prove a clean public consumer, set exact public versions and run:

```sh
INARI_API_VERSION=vX.Y.Z INARI_PLUGIN_SDK_VERSION=vX.Y.Z ./scripts/verify-public-consumer.sh
```

The script creates a temporary module with no `replace`, workspace, vendor, or relative checkout dependency, then exercises tenant invocation and re-auth through the public APIs.

## Lifecycle

- `pluginsdk.WithHooks(...)` — `OnInit` before serving, `OnShutdown` on SIGTERM/SIGINT.
- `pluginsdk.WithHealthChecker(...)` — backs the `HealthCheck` RPC the host uses for supervision.
- `pluginsdk.NewLogger(name)` — `slog` bridged into the host's structured log stream.

## Error semantics

Return `pluginsdk.Errorf(code, ...)` from handlers; codes map to the contract's `PluginError` (`INVALID_ARGUMENT`, `UNAUTHENTICATED`, `PERMISSION_DENIED`, `NOT_FOUND`, `FAILED_PRECONDITION`, `INTERNAL`, `UNAVAILABLE`). Unknown errors map to `INTERNAL`. Auth context validation fails closed with `UNAUTHENTICATED` before your handler runs.

## Versioning

Conventional Commits; releases via release-please (merge the Release PR → tag `vX.Y.Z` → `go get github.com/7K-Inari/inari-plugin-sdk@vX.Y.Z`). The `inari-api` contract module is pinned in `go.mod`; upgrade it deliberately.

Authoring guide (draft for inari-docs): [`docs/authoring-guide.md`](docs/authoring-guide.md).
