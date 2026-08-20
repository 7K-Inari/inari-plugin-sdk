package pluginsdk_test

import (
	"context"
	"errors"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
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

func (f *fakeHooks) OnInit(context.Context) error    { f.inited = true; return nil }
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
