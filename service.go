package pluginsdk

import (
	"context"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
)

// apiVersion is the contract version this SDK implements.
const apiVersion = "inari.plugin.v1"

// grpcService adapts a Plugin to the pluginv1 gRPC contract.
type grpcService struct {
	pluginv1.UnimplementedPluginContractServiceServer
	p *Plugin
}

// NewGRPCService exposes the plugin as the pluginv1 gRPC service. It is used
// by Serve internally and by testkit for in-process testing.
func NewGRPCService(p *Plugin) pluginv1.PluginContractServiceServer {
	return &grpcService{p: p}
}

func (s *grpcService) GetInfo(_ context.Context, _ *pluginv1.GetInfoRequest) (*pluginv1.GetInfoResponse, error) {
	return &pluginv1.GetInfoResponse{
		Info: &pluginv1.PluginInfo{
			Name:       s.p.info.Name,
			Version:    s.p.info.Version,
			ApiVersion: apiVersion,
		},
	}, nil
}

func (s *grpcService) GetCapabilities(_ context.Context, _ *pluginv1.GetCapabilitiesRequest) (*pluginv1.GetCapabilitiesResponse, error) {
	actions := s.p.Actions()
	out := make([]*pluginv1.Action, 0, len(actions))
	for _, a := range actions {
		out = append(out, &pluginv1.Action{
			Name:         a.Name,
			Description:  a.Description,
			InputSchema:  a.InputSchema,
			OutputSchema: a.OutputSchema,
		})
	}
	return &pluginv1.GetCapabilitiesResponse{Actions: out}, nil
}

func (s *grpcService) Invoke(ctx context.Context, req *pluginv1.InvokeRequest) (*pluginv1.InvokeResponse, error) {
	ac := AuthContextFromProto(req.GetAuthContext())
	if err := ac.Validate(); err != nil {
		return &pluginv1.InvokeResponse{Error: ProtoError(err)}, nil
	}
	action, ok := s.p.action(req.GetAction())
	if !ok {
		return &pluginv1.InvokeResponse{
			Error: ProtoError(Errorf(CodeNotFound, "action %q not found", req.GetAction())),
		}, nil
	}
	ctx = ContextWithAuth(ctx, ac)
	resp, err := action.Handler(ctx, &Request{Payload: req.GetPayload(), RequestID: req.GetRequestId()})
	if err != nil {
		return &pluginv1.InvokeResponse{Error: ProtoError(err)}, nil
	}
	if resp == nil {
		return &pluginv1.InvokeResponse{
			Error: ProtoError(Errorf(CodeInternal, "action %q returned a nil response", req.GetAction())),
		}, nil
	}
	return &pluginv1.InvokeResponse{Result: resp.Result}, nil
}

func (s *grpcService) HealthCheck(ctx context.Context, _ *pluginv1.HealthCheckRequest) (*pluginv1.HealthCheckResponse, error) {
	if s.p.health == nil {
		return &pluginv1.HealthCheckResponse{Status: pluginv1.HealthCheckResponse_SERVING_STATUS_SERVING}, nil
	}
	serving, msg := s.p.health.Health(ctx)
	status := pluginv1.HealthCheckResponse_SERVING_STATUS_NOT_SERVING
	if serving {
		status = pluginv1.HealthCheckResponse_SERVING_STATUS_SERVING
	}
	return &pluginv1.HealthCheckResponse{Status: status, Message: msg}, nil
}

// InitPlugin runs the plugin's OnInit hook. Called by Serve before the plugin
// starts accepting RPCs.
func InitPlugin(ctx context.Context, p *Plugin) error {
	if p.hooks == nil {
		return nil
	}
	return p.hooks.OnInit(ctx)
}

// ShutdownPlugin runs the plugin's OnShutdown hook. Called by Serve on
// graceful termination.
func ShutdownPlugin(ctx context.Context, p *Plugin) error {
	if p.hooks == nil {
		return nil
	}
	return p.hooks.OnShutdown(ctx)
}
