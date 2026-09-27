package pluginsdk_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"google.golang.org/grpc/metadata"
)

// captureCtx invokes the plugin's "probe" action with the given incoming
// metadata and returns the handler's ctx plus the invoke response.
func captureCtx(t *testing.T, p *pluginsdk.Plugin, md metadata.MD) (context.Context, *pluginv1.InvokeResponse) {
	t.Helper()
	var got context.Context
	if err := p.RegisterAction(pluginsdk.Action{
		Name: "probe",
		Handler: func(ctx context.Context, _ *pluginsdk.Request) (*pluginsdk.Response, error) {
			got = ctx
			return &pluginsdk.Response{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)
	resp, err := pluginsdk.NewGRPCService(p).Invoke(ctx, &pluginv1.InvokeRequest{
		Action:      "probe",
		RequestId:   "req-1",
		AuthContext: &pluginv1.AuthContext{PrincipalId: "user-1", TenantId: "org:acme"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return got, resp
}

func TestConnectionParsedFromMetadata(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	ctx, resp := captureCtx(t, p, metadata.Pairs(
		"x-inari-auth-method", "oidc-user",
		"x-inari-downstream-authorization", "Bearer secret-token-123",
		"x-inari-connection-config-server-url", "https://git.example.com",
	))
	if resp.GetError() != nil {
		t.Fatalf("invoke error = %+v", resp.GetError())
	}
	conn, ok := pluginsdk.ConnectionFrom(ctx)
	if !ok {
		t.Fatal("expected a connection")
	}
	if conn.AuthMethod != pluginsdk.AuthMethodOIDCUser {
		t.Fatalf("auth method = %v", conn.AuthMethod)
	}
	tok, ok := pluginsdk.DownstreamToken(ctx)
	if !ok || tok != "Bearer secret-token-123" {
		t.Fatalf("token = %q, %v", tok, ok)
	}
	v, ok := pluginsdk.ConnectionConfig(ctx, "server-url")
	if !ok || v != "https://git.example.com" {
		t.Fatalf("config = %q, %v", v, ok)
	}
}

func TestConnectionAbsent(t *testing.T) {
	ctx := context.Background()
	if _, ok := pluginsdk.ConnectionFrom(ctx); ok {
		t.Fatal("expected no connection")
	}
	if _, ok := pluginsdk.DownstreamToken(ctx); ok {
		t.Fatal("expected no token")
	}
	if _, ok := pluginsdk.ConnectionAuthMethod(ctx); ok {
		t.Fatal("expected no auth method")
	}
	if _, ok := pluginsdk.ConnectionConfig(ctx, "k"); ok {
		t.Fatal("expected no config")
	}
	if got := pluginsdk.ConnectionConfigs(ctx); got != nil {
		t.Fatalf("configs = %v", got)
	}
}

func TestConnectionConfigsCopy(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	ctx, _ := captureCtx(t, p, metadata.Pairs(
		"x-inari-connection-config-a", "1",
		"x-inari-connection-config-b", "2",
	))
	cfgs := pluginsdk.ConnectionConfigs(ctx)
	if len(cfgs) != 2 || cfgs["a"] != "1" || cfgs["b"] != "2" {
		t.Fatalf("configs = %v", cfgs)
	}
	cfgs["a"] = "mutated"
	if again := pluginsdk.ConnectionConfigs(ctx); again["a"] != "1" {
		t.Fatal("ConnectionConfigs must return a copy")
	}
}

func TestConnectionUnknownMethodWireName(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	ctx, resp := captureCtx(t, p, metadata.Pairs("x-inari-auth-method", "bogus"))
	if resp.GetError() != nil {
		t.Fatalf("invoke error = %+v", resp.GetError())
	}
	conn, ok := pluginsdk.ConnectionFrom(ctx)
	if !ok {
		t.Fatal("expected a connection")
	}
	if conn.AuthMethod != pluginsdk.AuthMethodUnspecified {
		t.Fatalf("auth method = %v", conn.AuthMethod)
	}
	if _, ok := pluginsdk.ConnectionAuthMethod(ctx); ok {
		t.Fatal("ConnectionAuthMethod should not be ok for an unknown wire name")
	}
}

func TestConnectionStringRedactsToken(t *testing.T) {
	const token = "Bearer secret-token-123"
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	ctx, _ := captureCtx(t, p, metadata.Pairs(
		"x-inari-auth-method", "oidc-user",
		"x-inari-downstream-authorization", token,
	))
	conn, _ := pluginsdk.ConnectionFrom(ctx)
	if s := conn.String(); strings.Contains(s, "secret-token-123") {
		t.Fatalf("String() leaks token: %q", s)
	}

	var buf bytes.Buffer
	logger := pluginsdk.NewLogger("test", pluginsdk.WithLogOutput(&buf), pluginsdk.WithLogLevel(slog.LevelDebug))
	logger.Info("invoking", "connection", conn.String(), "method", conn.AuthMethod.String())
	if strings.Contains(buf.String(), "secret-token-123") {
		t.Fatalf("log output leaks token: %q", buf.String())
	}
}
