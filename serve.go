package pluginsdk

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hashicorp/go-plugin"
	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	"google.golang.org/grpc"
)

// PluginName is the dispense key hosts use to obtain the contract client.
const PluginName = "extension"

// grpcPlugin adapts the SDK plugin to hashicorp go-plugin's gRPC transport.
type grpcPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	p *Plugin
}

func (g *grpcPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	pluginv1.RegisterPluginContractServiceServer(s, NewGRPCService(g.p))
	return nil
}

func (g *grpcPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, conn *grpc.ClientConn) (any, error) {
	return pluginv1.NewPluginContractServiceClient(conn), nil
}

// PluginSet returns the go-plugin dispense map for this plugin, keyed by
// PluginName. Shared by Serve and host-side launchers.
func (p *Plugin) PluginSet() plugin.PluginSet {
	return plugin.PluginSet{PluginName: &grpcPlugin{p: p}}
}

// ClientPluginSet returns the dispense map a host uses to connect to a plugin
// subprocess. It yields a pluginv1.PluginContractServiceClient.
func ClientPluginSet() plugin.PluginSet {
	return plugin.PluginSet{PluginName: &grpcPlugin{}}
}

// Serve runs the plugin as a go-plugin subprocess: handshake (magic cookie +
// protocol version), gRPC serving of the inari.plugin.v1 contract, lifecycle
// hooks, and graceful shutdown on SIGTERM/SIGINT. It blocks until the host
// terminates the connection or a termination signal arrives.
//
// Note: ctx cancellation (or a termination signal) only triggers the
// OnShutdown hook; it does not stop serving or exit the process. Process
// termination is the host's responsibility in the sidecar model.
func (p *Plugin) Serve(ctx context.Context) error {
	if err := InitPlugin(ctx, p); err != nil {
		return fmt.Errorf("plugin init: %w", err)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = ShutdownPlugin(context.Background(), p)
		case <-done:
		}
	}()
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake(),
		Plugins:         p.PluginSet(),
		GRPCServer:      plugin.DefaultGRPCServer,
	})
	return nil
}
