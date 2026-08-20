package pluginsdk

import (
	"context"
	"sort"
	"sync"
)

// Request is the input envelope handed to an action handler.
type Request struct {
	// Payload is the JSON-encoded action input.
	Payload []byte
	// RequestID is the host-assigned correlation id.
	RequestID string
}

// Response is the output envelope returned by an action handler.
type Response struct {
	// Result is the JSON-encoded action output.
	Result []byte
}

// Handler implements a single plugin action. The authenticated identity of the
// caller is available via AuthContextFrom(ctx).
type Handler func(ctx context.Context, req *Request) (*Response, error)

// Action declares one invocable capability of a plugin.
type Action struct {
	Name        string
	Description string
	// InputSchema / OutputSchema are optional JSON Schema documents used by the
	// control plane to validate and render forms.
	InputSchema  []byte
	OutputSchema []byte
	Handler      Handler
}

// Info identifies a plugin.
type Info struct {
	Name    string
	Version string
}

// Hooks receive lifecycle callbacks. All methods are optional.
type Hooks interface {
	// OnInit runs once before the plugin starts serving.
	OnInit(ctx context.Context) error
	// OnShutdown runs when the host requests graceful termination.
	OnShutdown(ctx context.Context) error
}

// HealthChecker reports plugin health for host supervision.
type HealthChecker interface {
	// Health reports readiness: serving, human-readable detail.
	Health(ctx context.Context) (serving bool, message string)
}

// Plugin is an Inari backend extension: identity, declared actions, and
// optional lifecycle hooks.
type Plugin struct {
	info    Info
	hooks   Hooks
	health  HealthChecker
	mu      sync.RWMutex
	actions map[string]Action
}

// Option configures a Plugin.
type Option func(*Plugin)

// WithHooks attaches lifecycle hooks.
func WithHooks(h Hooks) Option {
	return func(p *Plugin) { p.hooks = h }
}

// WithHealthChecker attaches a health checker used by the HealthCheck RPC.
func WithHealthChecker(h HealthChecker) Option {
	return func(p *Plugin) { p.health = h }
}

// New creates a plugin with the given identity.
func New(info Info, opts ...Option) *Plugin {
	p := &Plugin{info: info, actions: map[string]Action{}}
	for _, o := range opts {
		o(p)
	}
	return p
}

// RegisterAction declares an invocable action. Names must be unique.
func (p *Plugin) RegisterAction(a Action) error {
	if a.Name == "" {
		return Errorf(CodeInvalidArgument, "action name is required")
	}
	if a.Handler == nil {
		return Errorf(CodeInvalidArgument, "action %q requires a handler", a.Name)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.actions[a.Name]; exists {
		return Errorf(CodeInvalidArgument, "action %q already registered", a.Name)
	}
	p.actions[a.Name] = a
	return nil
}

// Actions returns the declared actions sorted by name.
func (p *Plugin) Actions() []Action {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Action, 0, len(p.actions))
	for _, a := range p.actions {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (p *Plugin) action(name string) (Action, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	a, ok := p.actions[name]
	return a, ok
}
