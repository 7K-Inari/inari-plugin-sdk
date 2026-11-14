package consumer_test

import (
	"context"
	"encoding/json"
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
)

type publicGatewayHandler struct {
	extensionsv1connect.UnimplementedAgentGatewayHandler
	auth       string
	tenant     string
	cluster    string
	credential string
	command    *agentv1.InvokeAction
	err        error
	calls      int
}

func (h *publicGatewayHandler) InvokeAction(ctx context.Context, req *agentv1.InvokeAction) (*agentv1.CommandAck, error) {
	h.calls++
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeInternal, "missing call info")
	}
	h.auth = info.RequestHeader().Get("Authorization")
	h.tenant = info.RequestHeader().Get(extensionsv1.MetadataTenant)
	h.cluster = info.RequestHeader().Get(extensionsv1.MetadataCluster)
	h.credential = info.RequestHeader().Get(extensionsv1.MetadataUserCredential)
	h.command = req
	if h.err != nil {
		return nil, h.err
	}
	return &agentv1.CommandAck{CommandId: req.GetCommandId(), Result: agentv1.CommandResult_COMMAND_RESULT_APPLIED}, nil
}

func TestPublicTenantInvocationAndReauth(t *testing.T) {
	handler := &publicGatewayHandler{}
	rpcServer := connect.NewServer()
	extensionsv1connect.RegisterAgentGatewayHandler(rpcServer, handler)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, rpcServer)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	gateway, err := agentgateway.NewClient(agentgateway.Config{
		BaseURL:     ts.URL,
		HTTPClient:  ts.Client(),
		TokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "extension-jwt", TokenType: "Bearer"}),
	})
	if err != nil {
		t.Fatal(err)
	}

	p := pluginsdk.New(pluginsdk.Info{Name: "public-consumer", Version: "0.1.0"}, pluginsdk.WithAuthMethods(pluginsdk.AuthMethod{
		Type: pluginsdk.AuthMethodOIDCSSOSession, Audience: "argocd", IsDefault: true,
	}))
	if err := p.RegisterAction(pluginsdk.Action{Name: "sync", Handler: func(ctx context.Context, _ *pluginsdk.Request) (*pluginsdk.Response, error) {
		ack, err := gateway.InvokeAction(ctx, "cluster-1", &agentv1.InvokeAction{CommandId: "command-1", Action: "sync"})
		if err != nil {
			return nil, err
		}
		return &pluginsdk.Response{Result: []byte(fmt.Sprintf(`{"result":%q}`, ack.GetResult().String()))}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	if err := p.RegisterAction(pluginsdk.Action{Name: "reauth", Handler: func(context.Context, *pluginsdk.Request) (*pluginsdk.Response, error) {
		return nil, pluginsdk.ReauthError("argocd")
	}}); err != nil {
		t.Fatal(err)
	}

	md := metadata.Pairs(
		pluginsdk.HeaderAuthMethod, "oidc-sso-session",
		pluginsdk.HeaderDownstreamAuthorization, "Bearer user-secret",
	)
	auth := &pluginv1.AuthContext{PrincipalId: "user-1", TenantId: "org:acme"}
	resp, err := pluginsdk.NewGRPCService(p).Invoke(metadata.NewIncomingContext(context.Background(), md), &pluginv1.InvokeRequest{Action: "sync", AuthContext: auth})
	if err != nil || resp.GetError() != nil {
		t.Fatalf("sync failed: %v %+v", err, resp.GetError())
	}
	if handler.auth != "Bearer extension-jwt" || handler.tenant != "org:acme" || handler.cluster != "cluster-1" {
		t.Fatalf("routing/auth = %q %q %q", handler.auth, handler.tenant, handler.cluster)
	}
	if handler.credential != "user-secret" {
		t.Fatalf("credential = %q", handler.credential)
	}
	if handler.command.GetUserCredentialRef() != "" || strings.Contains(fmt.Sprint(handler.command), "user-secret") {
		t.Fatalf("raw credential reached command: %+v", handler.command)
	}

	resp, err = pluginsdk.NewGRPCService(p).Invoke(metadata.NewIncomingContext(context.Background(), md), &pluginv1.InvokeRequest{Action: "reauth", AuthContext: auth})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	pluginsdk.WriteHTTPError(recorder, pluginsdk.ErrorFromProto(resp.GetError()))
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get(pluginsdk.HeaderReauth) != "argocd" {
		t.Fatalf("reauth response = %d %+v", recorder.Code, recorder.Header())
	}
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["detail"] != "re-authentication required" || body["code"] != pluginsdk.ReauthCode {
		t.Fatalf("reauth body = %+v", body)
	}

	_, err = gateway.InvokeAction(pluginsdk.ContextWithAuth(context.Background(), pluginsdk.AuthContext{PrincipalID: "user-1", TenantID: "org:acme"}), "cluster-1", &agentv1.InvokeAction{UserCredentialRef: "attacker-ref"})
	var pluginErr *pluginsdk.Error
	if !errors.As(err, &pluginErr) || pluginErr.Code != pluginsdk.CodeInvalidArgument {
		t.Fatalf("self-asserted credential ref err = %v", err)
	}

	handler.err = connect.NewError(connect.CodeUnavailable, "agent disconnected")
	_, err = gateway.InvokeAction(pluginsdk.ContextWithAuth(context.Background(), pluginsdk.AuthContext{PrincipalID: "user-1", TenantID: "org:acme"}), "cluster-1", &agentv1.InvokeAction{})
	if !errors.As(err, &pluginErr) || pluginErr.Code != pluginsdk.CodeUnavailable {
		t.Fatalf("disconnect err = %v", err)
	}
}
