package pluginsdk_test

import (
	"context"
	"errors"
	"testing"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"google.golang.org/grpc/metadata"
)

func newService(t *testing.T, p *pluginsdk.Plugin) pluginv1.PluginContractServiceServer {
	t.Helper()
	return pluginsdk.NewGRPCService(p)
}

func TestGetInfo(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "greeter", Version: "1.2.3"})
	resp, err := newService(t, p).GetInfo(context.Background(), &pluginv1.GetInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetInfo().GetName() != "greeter" || resp.GetInfo().GetVersion() != "1.2.3" {
		t.Fatalf("info = %+v", resp.GetInfo())
	}
	if resp.GetInfo().GetApiVersion() != "inari.plugin.v1" {
		t.Fatalf("api version = %q", resp.GetInfo().GetApiVersion())
	}
}

func TestGetCapabilities(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	if err := p.RegisterAction(pluginsdk.Action{
		Name:        "greet",
		Description: "says hi",
		InputSchema: []byte(`{"type":"object"}`),
		Handler:     okHandler,
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := newService(t, p).GetCapabilities(context.Background(), &pluginv1.GetCapabilitiesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetActions()) != 1 {
		t.Fatalf("actions = %+v", resp.GetActions())
	}
	a := resp.GetActions()[0]
	if a.GetName() != "greet" || a.GetDescription() != "says hi" || string(a.GetInputSchema()) != `{"type":"object"}` {
		t.Fatalf("action = %+v", a)
	}
}

func validInvoke(action string, payload []byte) *pluginv1.InvokeRequest {
	return &pluginv1.InvokeRequest{
		Action:      action,
		Payload:     payload,
		RequestId:   "req-1",
		AuthContext: &pluginv1.AuthContext{PrincipalId: "user-1", TenantId: "org:acme"},
	}
}

func TestInvokeHappyPath(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	var gotAC pluginsdk.AuthContext
	err := p.RegisterAction(pluginsdk.Action{
		Name: "echo",
		Handler: func(ctx context.Context, req *pluginsdk.Request) (*pluginsdk.Response, error) {
			ac, ok := pluginsdk.AuthContextFrom(ctx)
			if !ok {
				t.Error("auth context missing in handler")
			}
			gotAC = ac
			if req.RequestID != "req-1" {
				t.Errorf("request id = %q", req.RequestID)
			}
			return &pluginsdk.Response{Result: req.Payload}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := newService(t, p).Invoke(context.Background(), validInvoke("echo", []byte(`{"msg":"hi"}`)))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError() != nil {
		t.Fatalf("unexpected error: %+v", resp.GetError())
	}
	if string(resp.GetResult()) != `{"msg":"hi"}` {
		t.Fatalf("result = %s", resp.GetResult())
	}
	if gotAC.PrincipalID != "user-1" || gotAC.TenantID != "org:acme" {
		t.Fatalf("auth context = %+v", gotAC)
	}
}

func TestInvokeRejectsMissingAuthContext(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	if err := p.RegisterAction(pluginsdk.Action{Name: "echo", Handler: okHandler}); err != nil {
		t.Fatal(err)
	}
	for _, req := range []*pluginv1.InvokeRequest{
		{Action: "echo"},
		{Action: "echo", AuthContext: &pluginv1.AuthContext{TenantId: "org:acme"}},
		{Action: "echo", AuthContext: &pluginv1.AuthContext{PrincipalId: "user-1"}},
	} {
		resp, err := newService(t, p).Invoke(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
			t.Fatalf("code = %v, want UNAUTHENTICATED", resp.GetError().GetCode())
		}
	}
}

func TestInvokeUnknownAction(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	resp, err := newService(t, p).Invoke(context.Background(), validInvoke("nope", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_NOT_FOUND {
		t.Fatalf("code = %v, want NOT_FOUND", resp.GetError().GetCode())
	}
}

func TestInvokeMapsHandlerError(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	if err := p.RegisterAction(pluginsdk.Action{
		Name: "typed",
		Handler: func(context.Context, *pluginsdk.Request) (*pluginsdk.Response, error) {
			return nil, pluginsdk.Errorf(pluginsdk.CodeFailedPrecondition, "not ready")
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.RegisterAction(pluginsdk.Action{
		Name: "plain",
		Handler: func(context.Context, *pluginsdk.Request) (*pluginsdk.Response, error) {
			return nil, errors.New("kaboom")
		},
	}); err != nil {
		t.Fatal(err)
	}
	svc := newService(t, p)

	resp, _ := svc.Invoke(context.Background(), validInvoke("typed", nil))
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION {
		t.Fatalf("code = %v", resp.GetError().GetCode())
	}
	resp, _ = svc.Invoke(context.Background(), validInvoke("plain", nil))
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_INTERNAL {
		t.Fatalf("code = %v", resp.GetError().GetCode())
	}
}

func TestInvokeNilHandlerResponse(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	if err := p.RegisterAction(pluginsdk.Action{
		Name:    "nilresp",
		Handler: func(context.Context, *pluginsdk.Request) (*pluginsdk.Response, error) { return nil, nil },
	}); err != nil {
		t.Fatal(err)
	}
	resp, err := newService(t, p).Invoke(context.Background(), validInvoke("nilresp", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_INTERNAL {
		t.Fatalf("code = %v, want INTERNAL", resp.GetError().GetCode())
	}
}

func TestHealthCheck(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	resp, err := newService(t, p).HealthCheck(context.Background(), &pluginv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.HealthCheckResponse_SERVING_STATUS_SERVING {
		t.Fatalf("status = %v", resp.GetStatus())
	}
}

type fakeHealth struct{ serving bool }

func (f fakeHealth) Health(context.Context) (bool, string) { return f.serving, "degraded" }

func TestHealthCheckCustomChecker(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"}, pluginsdk.WithHealthChecker(fakeHealth{}))
	resp, err := newService(t, p).HealthCheck(context.Background(), &pluginv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != pluginv1.HealthCheckResponse_SERVING_STATUS_NOT_SERVING {
		t.Fatalf("status = %v", resp.GetStatus())
	}
	if resp.GetMessage() != "degraded" {
		t.Fatalf("message = %q", resp.GetMessage())
	}
}

type fakeHooks struct {
	inited, shutdown bool
}

func (f *fakeHooks) OnInit(context.Context) error     { f.inited = true; return nil }
func (f *fakeHooks) OnShutdown(context.Context) error { f.shutdown = true; return nil }

func TestInitRunsHooks(t *testing.T) {
	h := &fakeHooks{}
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"}, pluginsdk.WithHooks(h))
	if err := pluginsdk.InitPlugin(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if !h.inited {
		t.Fatal("OnInit not called")
	}
}

func TestShutdownRunsHooks(t *testing.T) {
	h := &fakeHooks{}
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"}, pluginsdk.WithHooks(h))
	if err := pluginsdk.ShutdownPlugin(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if !h.shutdown {
		t.Fatal("OnShutdown not called")
	}
}

func TestGetInfoAuthMethods(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(
			pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser, Audience: "https://git.example.com", IsDefault: true},
			pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodServiceAccount},
		))
	resp, err := newService(t, p).GetInfo(context.Background(), &pluginv1.GetInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ms := resp.GetAuthMethods()
	if len(ms) != 2 {
		t.Fatalf("auth methods = %+v", ms)
	}
	if ms[0].GetType() != pluginv1.AuthMethod_TYPE_OIDC_USER || !ms[0].GetIsDefault() || ms[0].GetAudience() != "https://git.example.com" {
		t.Fatalf("method[0] = %+v", ms[0])
	}
	if ms[1].GetType() != pluginv1.AuthMethod_TYPE_SERVICE_ACCOUNT {
		t.Fatalf("method[1] = %+v", ms[1])
	}
}

func TestGetInfoNoAuthMethodsBackwardCompat(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	resp, err := newService(t, p).GetInfo(context.Background(), &pluginv1.GetInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetAuthMethods()) != 0 {
		t.Fatalf("auth methods = %+v, want empty (host default oidc-user applies)", resp.GetAuthMethods())
	}
}

func TestGetInfoInvalidAuthMethods(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{}))
	if _, err := newService(t, p).GetInfo(context.Background(), &pluginv1.GetInfoRequest{}); err == nil {
		t.Fatal("expected error for invalid auth method declaration")
	}
}

func invokeWithMD(t *testing.T, p *pluginsdk.Plugin, md metadata.MD) *pluginv1.InvokeResponse {
	t.Helper()
	if err := p.RegisterAction(pluginsdk.Action{Name: "probe", Handler: okHandler}); err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(context.Background(), md)
	resp, err := newService(t, p).Invoke(ctx, validInvoke("probe", nil))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func declaredPlugin() *pluginsdk.Plugin {
	return pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"},
		pluginsdk.WithAuthMethods(
			pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodOIDCUser, IsDefault: true},
			pluginsdk.AuthMethod{Type: pluginsdk.AuthMethodServiceAccount},
		))
}

func TestInvokeDeclaredMethodWithCredential(t *testing.T) {
	resp := invokeWithMD(t, declaredPlugin(), metadata.Pairs(
		"x-inari-auth-method", "service-account",
		"x-inari-downstream-authorization", "Bearer tok",
	))
	if resp.GetError() != nil {
		t.Fatalf("unexpected error: %+v", resp.GetError())
	}
}

func TestInvokeDeclaredMissingMethodHeaderFallsBackToDefault(t *testing.T) {
	resp := invokeWithMD(t, declaredPlugin(), metadata.Pairs(
		"x-inari-downstream-authorization", "Bearer tok",
	))
	if resp.GetError() != nil {
		t.Fatalf("unexpected error: %+v", resp.GetError())
	}
}

func TestInvokeDeclaredUnsupportedMethod(t *testing.T) {
	resp := invokeWithMD(t, declaredPlugin(), metadata.Pairs(
		"x-inari-auth-method", "api-key",
		"x-inari-downstream-authorization", "Bearer tok",
	))
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION {
		t.Fatalf("code = %v, want FAILED_PRECONDITION", resp.GetError().GetCode())
	}
}

func TestInvokeDeclaredUnknownMethodName(t *testing.T) {
	resp := invokeWithMD(t, declaredPlugin(), metadata.Pairs(
		"x-inari-auth-method", "bogus",
		"x-inari-downstream-authorization", "Bearer tok",
	))
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_FAILED_PRECONDITION {
		t.Fatalf("code = %v, want FAILED_PRECONDITION", resp.GetError().GetCode())
	}
}

func TestInvokeDeclaredMissingCredential(t *testing.T) {
	resp := invokeWithMD(t, declaredPlugin(), metadata.Pairs(
		"x-inari-auth-method", "oidc-user",
	))
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
		t.Fatalf("code = %v, want UNAUTHENTICATED", resp.GetError().GetCode())
	}
}

func TestInvokeUndeclaredPluginPassesThrough(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	for _, md := range []metadata.MD{
		nil,
		metadata.Pairs("x-inari-auth-method", "bogus"),
		metadata.Pairs("x-inari-downstream-authorization", "Bearer tok"),
	} {
		if resp := invokeWithMD(t, p, md); resp.GetError() != nil {
			t.Fatalf("undeclared plugin must not enforce: %+v", resp.GetError())
		}
		// fresh plugin per iteration: RegisterAction rejects duplicates
		p = pluginsdk.New(pluginsdk.Info{Name: "x", Version: "0.0.1"})
	}
}
