// Package agentgateway provides the typed extension-side client for imperative
// operations on tenant clusters.
package agentgateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	agentv1 "github.com/7K-Inari/inari-api/gen/go/inari/agent/v1"
	extensionsv1 "github.com/7K-Inari/inari-api/gen/go/inari/extensions/v1"
	"github.com/7K-Inari/inari-api/gen/go/inari/extensions/v1/extensionsv1connect"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"golang.org/x/oauth2"
	"google.golang.org/protobuf/proto"
)

// Config configures the typed AgentGateway client.
type Config struct {
	// BaseURL is the absolute HTTP(S) control-plane base URL. TLS customization
	// belongs in HTTPClient, not in a separate raw-gRPC dial API.
	BaseURL string
	// HTTPClient optionally supplies custom TLS, proxy, or test transport.
	HTTPClient connecthttp.HTTPClient
	// TokenSource provides the per-extension client_credentials identity. A
	// clientcredentials.Config.TokenSource is the production default.
	TokenSource oauth2.TokenSource
}

// Gateway invokes one command without exposing transport or metadata handling.
type Gateway interface {
	InvokeAction(ctx context.Context, clusterID string, command *agentv1.InvokeAction) (*agentv1.CommandAck, error)
}

// Client is a typed AgentGateway client.
type Client struct {
	client      extensionsv1connect.AgentGatewayClient
	tokenSource oauth2.TokenSource
}

var _ Gateway = (*Client)(nil)

// NewClient constructs a typed AgentGateway client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("agent gateway base URL is required")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("agent gateway base URL is invalid")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("agent gateway base URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if cfg.TokenSource == nil {
		return nil, fmt.Errorf("agent gateway token source is required")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	transport := connecthttp.NewTransport(httpClient, cfg.BaseURL, connecthttp.WithReadMaxBytes(0))
	return &Client{
		client:      extensionsv1connect.NewAgentGatewayClient(connect.NewClient(transport)),
		tokenSource: cfg.TokenSource,
	}, nil
}

// InvokeAction invokes a command using the authenticated plugin invocation context.
// Tenant identity and any downstream credential come from ctx, never from the
// command payload. The client does not retry imperative operations.
func (c *Client) InvokeAction(ctx context.Context, clusterID string, command *agentv1.InvokeAction) (*agentv1.CommandAck, error) {
	ac, ok := pluginsdk.AuthContextFrom(ctx)
	if !ok {
		return nil, pluginsdk.Errorf(pluginsdk.CodeUnauthenticated, "agent gateway requires authenticated invocation context")
	}
	if err := ac.Validate(); err != nil {
		return nil, err
	}
	if clusterID == "" {
		return nil, pluginsdk.Errorf(pluginsdk.CodeInvalidArgument, "agent gateway cluster ID is required")
	}
	if command == nil {
		return nil, pluginsdk.Errorf(pluginsdk.CodeInvalidArgument, "agent gateway command is required")
	}
	if command.GetUserCredentialRef() != "" {
		return nil, pluginsdk.Errorf(pluginsdk.CodeInvalidArgument, "agent gateway credential references are control-plane owned; use invocation context")
	}
	credential, err := downstreamCredential(ctx)
	if err != nil {
		return nil, err
	}
	token, err := c.tokenSource.Token()
	if err != nil || token == nil || token.AccessToken == "" {
		return nil, pluginsdk.Errorf(pluginsdk.CodeUnauthenticated, "agent gateway identity token unavailable")
	}

	// Clone so transport/server-side command ID and credential-reference
	// normalization can never mutate the caller's command.
	request := proto.Clone(command).(*agentv1.InvokeAction)
	callCtx, info := connect.NewClientContext(ctx)
	info.RequestHeader().Set("Authorization", "Bearer "+token.AccessToken)
	info.RequestHeader().Set(extensionsv1.MetadataTenant, ac.TenantID)
	info.RequestHeader().Set(extensionsv1.MetadataCluster, clusterID)
	if credential != "" {
		info.RequestHeader().Set(extensionsv1.MetadataUserCredential, credential)
	}
	ack, err := c.client.InvokeAction(callCtx, request)
	if err != nil {
		return nil, mapConnectError(err)
	}
	return ack, nil
}

func downstreamCredential(ctx context.Context) (string, error) {
	credential, ok := pluginsdk.DownstreamToken(ctx)
	if !ok {
		return "", nil
	}
	credential = strings.TrimSpace(credential)
	if scheme, rest, found := strings.Cut(credential, " "); found && strings.EqualFold(scheme, "bearer") {
		credential = strings.TrimSpace(rest)
	}
	if credential == "" || strings.EqualFold(credential, "bearer") {
		return "", pluginsdk.Errorf(pluginsdk.CodeUnauthenticated, "downstream credential is empty")
	}
	return credential, nil
}

func mapConnectError(err error) error {
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %w", pluginsdk.Errorf(pluginsdk.CodeUnavailable, "agent gateway deadline exceeded"), context.DeadlineExceeded)
		}
		if errors.Is(err, context.Canceled) {
			return fmt.Errorf("%w: %w", pluginsdk.Errorf(pluginsdk.CodeUnavailable, "agent gateway invocation canceled"), context.Canceled)
		}
		return pluginsdk.Errorf(pluginsdk.CodeUnavailable, "agent gateway transport unavailable")
	}
	code := pluginsdk.CodeInternal
	switch connectErr.Code() {
	case connect.CodeInvalidArgument:
		code = pluginsdk.CodeInvalidArgument
	case connect.CodeUnauthenticated:
		code = pluginsdk.CodeUnauthenticated
	case connect.CodePermissionDenied:
		code = pluginsdk.CodePermissionDenied
	case connect.CodeNotFound:
		code = pluginsdk.CodeNotFound
	case connect.CodeFailedPrecondition:
		code = pluginsdk.CodeFailedPrecondition
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeCanceled:
		code = pluginsdk.CodeUnavailable
	}
	pluginErr := pluginsdk.Errorf(code, "agent gateway invoke failed: %s", connectErr.Message())
	switch connectErr.Code() {
	case connect.CodeDeadlineExceeded:
		return fmt.Errorf("%w: %w", pluginErr, context.DeadlineExceeded)
	case connect.CodeCanceled:
		return fmt.Errorf("%w: %w", pluginErr, context.Canceled)
	default:
		return pluginErr
	}
}
