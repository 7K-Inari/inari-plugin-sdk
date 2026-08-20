// Package testkit lets plugin authors test their plugins in-process, without
// building or launching a subprocess: it serves the plugin over an in-memory
// gRPC connection and exposes the same client surface as the host.
package testkit

import (
	"context"
	"net"
	"testing"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// Client calls a plugin served in-process.
type Client struct {
	conn     *grpc.ClientConn
	contract pluginv1.PluginContractServiceClient
}

// Run serves p in-process (running its init hook) and returns a connected
// client. The server is shut down when the test finishes.
func Run(t testing.TB, p *pluginsdk.Plugin) *Client {
	t.Helper()
	if err := pluginsdk.InitPlugin(context.Background(), p); err != nil {
		t.Fatalf("plugin init: %v", err)
	}
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pluginv1.RegisterPluginContractServiceServer(srv, pluginsdk.NewGRPCService(p))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop() })

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &Client{conn: conn, contract: pluginv1.NewPluginContractServiceClient(conn)}
}

// Info returns the plugin's identity and contract version.
func (c *Client) Info(ctx context.Context) (*pluginv1.PluginInfo, error) {
	resp, err := c.contract.GetInfo(ctx, &pluginv1.GetInfoRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetInfo(), nil
}

// Capabilities returns the plugin's declared actions.
func (c *Client) Capabilities(ctx context.Context) ([]*pluginv1.Action, error) {
	resp, err := c.contract.GetCapabilities(ctx, &pluginv1.GetCapabilitiesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.GetActions(), nil
}

// Invoke calls an action with a fake authenticated identity. Plugin-reported
// failures are returned as *pluginsdk.Error.
func (c *Client) Invoke(ctx context.Context, action string, ac pluginsdk.AuthContext, payload []byte) (*pluginsdk.Response, error) {
	resp, err := c.contract.Invoke(ctx, &pluginv1.InvokeRequest{
		Action:      action,
		AuthContext: ac.Proto(),
		Payload:     payload,
	})
	if err != nil {
		return nil, err
	}
	if pe := resp.GetError(); pe != nil {
		return nil, &pluginsdk.Error{
			Code:    pluginsdk.Code(pe.GetCode()),
			Message: pe.GetMessage(),
			Details: pe.GetDetails(),
		}
	}
	return &pluginsdk.Response{Result: resp.GetResult()}, nil
}

// Health reports the plugin's serving status.
func (c *Client) Health(ctx context.Context) (serving bool, message string, err error) {
	resp, err := c.contract.HealthCheck(ctx, &pluginv1.HealthCheckRequest{})
	if err != nil {
		return false, "", err
	}
	return resp.GetStatus() == pluginv1.HealthCheckResponse_SERVING_STATUS_SERVING, resp.GetMessage(), nil
}
