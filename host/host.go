// Package host is the reference host-side client for launching and supervising
// Inari plugin subprocesses (platform plan §5.8): handshake, protocol version
// negotiation, SHA-256 checksum verification, and crash isolation. The
// inari-server Extension Host integrates against the same contract; this
// package also backs the SDK's integration tests and testkit.
package host

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrPluginCrashed is returned when the plugin subprocess exits unexpectedly
// before or during a call.
var ErrPluginCrashed = errors.New("plugin process exited")

// Checksum returns the SHA-256 digest of a plugin binary.
func Checksum(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// LaunchOption configures Launch.
type LaunchOption func(*launchConfig)

type launchConfig struct {
	checksum []byte
	env      []string
}

// WithChecksum pins the expected SHA-256 of the plugin binary. Launch refuses
// to start a binary whose digest differs (§5.8 checksum verification).
func WithChecksum(sum []byte) LaunchOption {
	return func(c *launchConfig) { c.checksum = sum }
}

// WithEnv adds environment variables to the plugin process.
func WithEnv(env ...string) LaunchOption {
	return func(c *launchConfig) { c.env = append(c.env, env...) }
}

// Launch starts the plugin binary as a managed go-plugin subprocess and
// completes the handshake. By default the binary's current SHA-256 is pinned.
func Launch(ctx context.Context, path string, opts ...LaunchOption) (*Client, error) {
	cfg := launchConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.checksum == nil {
		sum, err := Checksum(path)
		if err != nil {
			return nil, fmt.Errorf("checksum plugin binary: %w", err)
		}
		cfg.checksum = sum
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = append(os.Environ(), cfg.env...)
	proc := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig:  pluginsdk.Handshake(),
		Plugins:          pluginsdk.ClientPluginSet(),
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		Managed:          true,
		SecureConfig: &plugin.SecureConfig{
			Checksum: cfg.checksum,
			Hash:     sha256.New(),
		},
	})
	rpc, err := proc.Client()
	if err != nil {
		proc.Kill()
		return nil, fmt.Errorf("handshake with plugin: %w", err)
	}
	raw, err := rpc.Dispense(pluginsdk.PluginName)
	if err != nil {
		proc.Kill()
		return nil, fmt.Errorf("dispense %q: %w", pluginsdk.PluginName, err)
	}
	contract, ok := raw.(pluginv1.PluginContractServiceClient)
	if !ok {
		proc.Kill()
		return nil, fmt.Errorf("unexpected client type %T", raw)
	}
	return &Client{proc: proc, contract: contract}, nil
}

// Client is a supervised handle to a running plugin subprocess.
type Client struct {
	proc     *plugin.Client
	contract pluginv1.PluginContractServiceClient
}

// Info returns the plugin's identity and contract version.
func (c *Client) Info(ctx context.Context) (*pluginv1.PluginInfo, error) {
	resp, err := c.contract.GetInfo(ctx, &pluginv1.GetInfoRequest{})
	if err != nil {
		return nil, c.wrapErr(err)
	}
	return resp.GetInfo(), nil
}

// Capabilities returns the plugin's declared actions.
func (c *Client) Capabilities(ctx context.Context) ([]*pluginv1.Action, error) {
	resp, err := c.contract.GetCapabilities(ctx, &pluginv1.GetCapabilitiesRequest{})
	if err != nil {
		return nil, c.wrapErr(err)
	}
	return resp.GetActions(), nil
}

// Invoke calls an action with the authenticated identity of the caller. A
// plugin-reported failure is returned as *pluginsdk.Error; a crashed plugin
// process surfaces as ErrPluginCrashed.
func (c *Client) Invoke(ctx context.Context, action string, ac pluginsdk.AuthContext, payload []byte) (*pluginsdk.Response, error) {
	resp, err := c.contract.Invoke(ctx, &pluginv1.InvokeRequest{
		Action:      action,
		AuthContext: ac.Proto(),
		Payload:     payload,
	})
	if err != nil {
		return nil, c.wrapErr(err)
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
		return false, "", c.wrapErr(err)
	}
	return resp.GetStatus() == pluginv1.HealthCheckResponse_SERVING_STATUS_SERVING, resp.GetMessage(), nil
}

// Exited reports whether the plugin process has terminated.
func (c *Client) Exited() bool {
	return c.proc.Exited()
}

// Close terminates the plugin process.
func (c *Client) Close() {
	c.proc.Kill()
}

func (c *Client) wrapErr(err error) error {
	if c.proc.Exited() {
		return fmt.Errorf("%w: %v", ErrPluginCrashed, err)
	}
	// A crashed plugin surfaces as transport Unavailable before the supervisor
	// observes the exit; poll briefly so callers get a stable ErrPluginCrashed.
	if status.Code(err) == codes.Unavailable {
		for range 20 {
			if c.proc.Exited() {
				return fmt.Errorf("%w: %v", ErrPluginCrashed, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	return err
}
