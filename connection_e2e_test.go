package pluginsdk_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/testkit"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var e2eAC = pluginsdk.AuthContext{PrincipalID: "user-1", TenantID: "org:acme"}

// Full gRPC round trip through testkit: outgoing metadata reaches the handler.
func TestConnectionEndToEndViaTestkit(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(
			pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser, IsDefault: true},
			pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodServiceAccount},
		))
	var gotTok, gotMethod, gotCfg string
	if err := p.RegisterAction(pluginsdk.Action{
		Name: "probe",
		Handler: func(ctx context.Context, _ *pluginsdk.Request) (*pluginsdk.Response, error) {
			gotTok, _ = pluginsdk.DownstreamToken(ctx)
			m, _ := pluginsdk.ConnectionAuthMethod(ctx)
			gotMethod = m.String()
			gotCfg, _ = pluginsdk.ConnectionConfig(ctx, "server-url")
			return &pluginsdk.Response{}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	c := testkit.Run(t, p)
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"x-inari-auth-method", "service-account",
		"x-inari-downstream-authorization", "Bearer qa-token",
		"x-inari-connection-config-server-url", "https://git.example.com",
	)
	if _, err := c.Invoke(ctx, "probe", e2eAC, nil); err != nil {
		t.Fatal(err)
	}
	if gotTok != "Bearer qa-token" || gotMethod != "service-account" || gotCfg != "https://git.example.com" {
		t.Fatalf("got token=%q method=%q cfg=%q", gotTok, gotMethod, gotCfg)
	}
}

// Declared plugin with no metadata at all: missing credential is UNAUTHENTICATED.
func TestInvokeDeclaredNoMetadata(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser}))
	_ = p.RegisterAction(pluginsdk.Action{Name: "a", Handler: okHandler})
	c := testkit.Run(t, p)
	_, err := c.Invoke(context.Background(), "a", e2eAC, nil)
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeUnauthenticated {
		t.Fatalf("err = %v, want UNAUTHENTICATED", err)
	}
}

// An empty auth-method header value is fail-closed, not treated as absent.
func TestInvokeEmptyAuthMethodHeaderValue(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser}))
	_ = p.RegisterAction(pluginsdk.Action{Name: "a", Handler: okHandler})
	c := testkit.Run(t, p)
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"x-inari-auth-method", "",
		"x-inari-downstream-authorization", "Bearer t",
	)
	_, err := c.Invoke(ctx, "a", e2eAC, nil)
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeFailedPrecondition {
		t.Fatalf("err = %v, want FAILED_PRECONDITION", err)
	}
}

// Default resolution: IsDefault wins over declaration order; with no IsDefault
// the first declared method wins.
func TestConnectionDefaultResolutionOrder(t *testing.T) {
	probe := func(methods ...pluginsdk.AuthMethod) (func(ctx context.Context) (string, error), *string) {
		var got string
		p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"}, pluginsdk.WithAuthMethods(methods...))
		_ = p.RegisterAction(pluginsdk.Action{
			Name: "a",
			Handler: func(ctx context.Context, _ *pluginsdk.Request) (*pluginsdk.Response, error) {
				m, _ := pluginsdk.ConnectionAuthMethod(ctx)
				got = m.String()
				return &pluginsdk.Response{}, nil
			},
		})
		c := testkit.Run(t, p)
		return func(ctx context.Context) (string, error) {
			_, err := c.Invoke(ctx, "a", e2eAC, nil)
			return got, err
		}, &got
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-inari-downstream-authorization", "Bearer t")

	invoke, got := probe(
		pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodAPIKey},
		pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser, IsDefault: true},
	)
	if _, err := invoke(ctx); err != nil {
		t.Fatal(err)
	}
	if *got != "oidc-user" {
		t.Fatalf("default = %q, want oidc-user", *got)
	}

	invoke, got = probe(
		pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodAPIKey},
		pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser},
	)
	if _, err := invoke(ctx); err != nil {
		t.Fatal(err)
	}
	if *got != "api-key" {
		t.Fatalf("default = %q, want api-key", *got)
	}
}

// Concurrent invokes with different tokens must not cross-contaminate.
func TestInvokeConcurrentConnectionIsolation(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser}))
	_ = p.RegisterAction(pluginsdk.Action{
		Name: "a",
		Handler: func(ctx context.Context, _ *pluginsdk.Request) (*pluginsdk.Response, error) {
			tok, ok := pluginsdk.DownstreamToken(ctx)
			if !ok {
				return nil, pluginsdk.Errorf(pluginsdk.CodeInternal, "no token")
			}
			return &pluginsdk.Response{Result: []byte(tok)}, nil
		},
	})
	c := testkit.Run(t, p)
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			want := fmt.Sprintf("Bearer tok-%d", i)
			ctx := metadata.AppendToOutgoingContext(context.Background(),
				"x-inari-downstream-authorization", want)
			resp, err := c.Invoke(ctx, "a", e2eAC, nil)
			if err != nil {
				errs <- err
				return
			}
			if string(resp.Result) != want {
				errs <- fmt.Errorf("got %q, want %q", resp.Result, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// An undeclared plugin still receives parsed connection metadata (pass-through).
func TestInvokeUndeclaredPluginSeesConnection(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	var got string
	var ok bool
	_ = p.RegisterAction(pluginsdk.Action{
		Name: "a",
		Handler: func(ctx context.Context, _ *pluginsdk.Request) (*pluginsdk.Response, error) {
			got, ok = pluginsdk.DownstreamToken(ctx)
			return &pluginsdk.Response{}, nil
		},
	})
	c := testkit.Run(t, p)
	ctx := metadata.AppendToOutgoingContext(context.Background(), "x-inari-downstream-authorization", "Bearer legacy")
	if _, err := c.Invoke(ctx, "a", e2eAC, nil); err != nil {
		t.Fatal(err)
	}
	if !ok || got != "Bearer legacy" {
		t.Fatalf("token = %q, %v", got, ok)
	}
}

// Enforcement error messages must not echo credential values.
func TestInvokeErrorMessagesRedactCredential(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser}))
	_ = p.RegisterAction(pluginsdk.Action{Name: "a", Handler: okHandler})
	c := testkit.Run(t, p)
	const secret = "super-secret-value-xyz"
	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"x-inari-auth-method", "api-key",
		"x-inari-downstream-authorization", secret,
	)
	_, err := c.Invoke(ctx, "a", e2eAC, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaks credential: %v", err)
	}
}

// An invalid auth-method declaration surfaces as INVALID_ARGUMENT over gRPC.
func TestGetInfoInvalidDeclStatusCode(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{}))
	c := testkit.Run(t, p)
	_, err := c.Info(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid declaration")
	}
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Fatalf("status code = %v, want InvalidArgument", code)
	}
}
