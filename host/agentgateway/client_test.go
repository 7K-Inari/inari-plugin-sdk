package agentgateway_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	extensionsv1 "github.com/7K-Inari/inari-api/gen/go/inari/extensions/v1"
	"github.com/7K-Inari/inari-api/gen/go/inari/extensions/v1/extensionsv1connect"
	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/host/agentgateway"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type gatewayHandler struct {
	extensionsv1connect.UnimplementedAgentGatewayHandler
	auth       string
	tenant     string
	cluster    string
	credential string
	command    *agentv1.InvokeAction
	err        error
	calls      int
}

func (h *gatewayHandler) InvokeAction(ctx context.Context, req *agentv1.InvokeAction) (*agentv1.CommandAck, error) {
	h.calls++
	if info, ok := connect.CallInfoForServerContext(ctx); ok {
		header := info.RequestHeader()
		h.auth = header.Get("Authorization")
		h.tenant = header.Get(extensionsv1.MetadataTenant)
		h.cluster = header.Get(extensionsv1.MetadataCluster)
		h.credential = header.Get(extensionsv1.MetadataUserCredential)
	}
	h.command = req
	if h.err != nil {
		return nil, h.err
	}
	return &agentv1.CommandAck{CommandId: req.GetCommandId(), Result: agentv1.CommandResult_COMMAND_RESULT_APPLIED}, nil
}

func newGatewayServer(t *testing.T) (*gatewayHandler, string, *http.Client) {
	t.Helper()
	handler := &gatewayHandler{}
	server := connect.NewServer()
	extensionsv1connect.RegisterAgentGatewayHandler(server, handler)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return handler, ts.URL, ts.Client()
}

func newClient(t *testing.T, baseURL string, httpClient connecthttp.HTTPClient, tokenSource oauth2.TokenSource) *agentgateway.Client {
	t.Helper()
	client, err := agentgateway.NewClient(agentgateway.Config{BaseURL: baseURL, HTTPClient: httpClient, TokenSource: tokenSource})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func staticToken(value string) oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: value, TokenType: "Bearer"})
}

func authContext() context.Context {
	return pluginsdk.ContextWithAuth(context.Background(), pluginsdk.AuthContext{PrincipalID: "user-1", TenantID: "org:acme"})
}

func invokeThroughPlugin(t *testing.T, gateway agentgateway.Gateway, downstream string, command *agentv1.InvokeAction) (*pluginv1.InvokeResponse, *agentv1.CommandAck) {
	t.Helper()
	var ack *agentv1.CommandAck
	p := pluginsdk.New(pluginsdk.Info{Name: "consumer", Version: "0.1.0"}, pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{
		Type: pluginsdk.AuthMethodOIDCSSOSession, Audience: "argocd", IsDefault: true,
	}))
	if err := p.RegisterAction(pluginsdk.Action{Name: "sync", Handler: func(ctx context.Context, _ *pluginsdk.Request) (*pluginsdk.Response, error) {
		var err error
		ack, err = gateway.InvokeAction(ctx, "cluster-1", command)
		if err != nil {
			return nil, err
		}
		return &pluginsdk.Response{Result: []byte(`{"ok":true}`)}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	md := metadata.Pairs(
		pluginsdk.HeaderAuthMethod, "oidc-sso-session",
		pluginsdk.HeaderDownstreamAuthorization, downstream,
	)
	resp, err := pluginsdk.NewGRPCService(p).Invoke(metadata.NewIncomingContext(context.Background(), md), &pluginv1.InvokeRequest{
		Action:      "sync",
		AuthContext: &pluginv1.AuthContext{PrincipalId: "user-1", TenantId: "org:acme"},
	})
	if err != nil {
		t.Fatalf("plugin Invoke: %v", err)
	}
	return resp, ack
}

func TestNewClientValidation(t *testing.T) {
	for name, cfg := range map[string]agentgateway.Config{
		"missing URL":          {},
		"relative URL":         {BaseURL: "localhost:8080", TokenSource: staticToken("ext")},
		"unsupported scheme":   {BaseURL: "dns:///gateway", TokenSource: staticToken("ext")},
		"missing token source": {BaseURL: "https://gateway.example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := agentgateway.NewClient(cfg); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestInvokeActionUsesAuthenticatedContextAndMetadata(t *testing.T) {
	handler, baseURL, httpClient := newGatewayServer(t)
	client := newClient(t, baseURL, httpClient, staticToken("extension-jwt"))
	command := &agentv1.InvokeAction{CommandId: "client-command", Action: "sync"}
	original := proto.Clone(command).(*agentv1.InvokeAction)

	resp, ack := invokeThroughPlugin(t, client, "Bearer user-token", command)
	if resp.GetError() != nil {
		t.Fatalf("plugin error = %+v", resp.GetError())
	}
	if ack.GetResult() != agentv1.CommandResult_COMMAND_RESULT_APPLIED {
		t.Fatalf("ack = %+v", ack)
	}
	if handler.auth != "Bearer extension-jwt" {
		t.Errorf("Authorization = %q", handler.auth)
	}
	if handler.tenant != "org:acme" || handler.cluster != "cluster-1" {
		t.Errorf("routing = %q/%q", handler.tenant, handler.cluster)
	}
	if handler.credential != "user-token" {
		t.Errorf("credential = %q, want raw token without Bearer", handler.credential)
	}
	if handler.command.GetAction() != "sync" {
		t.Errorf("command = %+v", handler.command)
	}
	if !proto.Equal(command, original) {
		t.Errorf("client mutated caller command: got %+v want %+v", command, original)
	}
}

func TestInvokeActionRejectsInvalidInputs(t *testing.T) {
	client := newClient(t, "https://gateway.example.com", nil, staticToken("ext"))
	for name, tc := range map[string]struct {
		ctx      context.Context
		cluster  string
		command  *agentv1.InvokeAction
		wantCode pluginsdk.Code
	}{
		"missing auth":                 {context.Background(), "cluster-1", &agentv1.InvokeAction{}, pluginsdk.CodeUnauthenticated},
		"missing cluster":              {authContext(), "", &agentv1.InvokeAction{}, pluginsdk.CodeInvalidArgument},
		"nil command":                  {authContext(), "cluster-1", nil, pluginsdk.CodeInvalidArgument},
		"self asserted credential ref": {authContext(), "cluster-1", &agentv1.InvokeAction{UserCredentialRef: "attacker-ref"}, pluginsdk.CodeInvalidArgument},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := client.InvokeAction(tc.ctx, tc.cluster, tc.command)
			var pluginErr *pluginsdk.Error
			if !errors.As(err, &pluginErr) || pluginErr.Code != tc.wantCode {
				t.Fatalf("err = %v, want code %v", err, tc.wantCode)
			}
		})
	}
}

func TestInvokeActionRejectsEmptyDownstreamCredential(t *testing.T) {
	handler, baseURL, httpClient := newGatewayServer(t)
	client := newClient(t, baseURL, httpClient, staticToken("ext"))
	resp, _ := invokeThroughPlugin(t, client, "Bearer", &agentv1.InvokeAction{Action: "sync"})
	if resp.GetError().GetCode() != pluginv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED {
		t.Fatalf("error = %+v, want UNAUTHENTICATED", resp.GetError())
	}
	if handler.calls != 0 {
		t.Fatal("gateway was called with an empty credential")
	}
}

type failingTokenSource struct{ err error }

func (f failingTokenSource) Token() (*oauth2.Token, error) { return nil, f.err }

func TestTokenSourceFailureIsTypedAndRedacted(t *testing.T) {
	client := newClient(t, "https://gateway.example.com", nil, failingTokenSource{err: errors.New("secret-client-token")})
	_, err := client.InvokeAction(authContext(), "cluster-1", &agentv1.InvokeAction{})
	var pluginErr *pluginsdk.Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != pluginsdk.CodeUnauthenticated {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret-client-token") {
		t.Fatalf("error leaks token source detail: %v", err)
	}
}

func TestConnectErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		code       connect.Code
		want       pluginsdk.Code
		wantTarget error
	}{
		{connect.CodeInvalidArgument, pluginsdk.CodeInvalidArgument, nil},
		{connect.CodeUnauthenticated, pluginsdk.CodeUnauthenticated, nil},
		{connect.CodePermissionDenied, pluginsdk.CodePermissionDenied, nil},
		{connect.CodeNotFound, pluginsdk.CodeNotFound, nil},
		{connect.CodeFailedPrecondition, pluginsdk.CodeFailedPrecondition, nil},
		{connect.CodeUnavailable, pluginsdk.CodeUnavailable, nil},
		{connect.CodeDeadlineExceeded, pluginsdk.CodeUnavailable, context.DeadlineExceeded},
		{connect.CodeCanceled, pluginsdk.CodeUnavailable, context.Canceled},
		{connect.CodeInternal, pluginsdk.CodeInternal, nil},
	} {
		t.Run(fmt.Sprint(tc.code), func(t *testing.T) {
			handler, baseURL, httpClient := newGatewayServer(t)
			handler.err = connect.NewError(tc.code, "safe transport detail")
			client := newClient(t, baseURL, httpClient, staticToken("ext"))
			_, err := client.InvokeAction(authContext(), "cluster-1", &agentv1.InvokeAction{})
			var pluginErr *pluginsdk.Error
			if !errors.As(err, &pluginErr) || pluginErr.Code != tc.want {
				t.Fatalf("err = %v, want code %v", err, tc.want)
			}
			if tc.wantTarget != nil && !errors.Is(err, tc.wantTarget) {
				t.Fatalf("err = %v, want target %v", err, tc.wantTarget)
			}
		})
	}
}
