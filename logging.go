package pluginsdk

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/hashicorp/go-hclog"
)

// LoggerOption configures the logger returned by NewLogger.
type LoggerOption func(*loggerConfig)

type loggerConfig struct {
	output io.Writer
	level  slog.Level
}

// WithLogOutput redirects log output (defaults to stderr, which go-plugin
// streams into the host's log sink).
func WithLogOutput(w io.Writer) LoggerOption {
	return func(c *loggerConfig) { c.output = w }
}

// WithLogLevel sets the minimum level (defaults to info).
func WithLogLevel(l slog.Level) LoggerOption {
	return func(c *loggerConfig) { c.level = l }
}

// NewLogger returns a slog.Logger whose records are bridged into the host's
// structured log stream via hclog over the plugin's stderr (§5.8: the host
// captures plugin stdio and re-logs with plugin identity).
func NewLogger(name string, opts ...LoggerOption) *slog.Logger {
	cfg := loggerConfig{output: os.Stderr, level: slog.LevelInfo}
	for _, o := range opts {
		o(&cfg)
	}
	hc := hclog.New(&hclog.LoggerOptions{
		Name:   name,
		Output: cfg.output,
		Level:  hclog.LevelFromString(cfg.level.String()),
	})
	return slog.New(&hclogHandler{logger: hc, level: cfg.level})
}

// hclogHandler adapts slog records onto an hclog logger.
type hclogHandler struct {
	logger hclog.Logger
	level  slog.Level
	attrs  []slog.Attr
	groups []string
}

func (h *hclogHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *hclogHandler) Handle(_ context.Context, r slog.Record) error {
	args := make([]any, 0, (r.NumAttrs()+len(h.attrs))*2)
	for _, a := range h.attrs {
		args = append(args, a.Key, a.Value.Any())
	}
	r.Attrs(func(a slog.Attr) bool {
		args = append(args, a.Key, a.Value.Any())
		return true
	})
	switch {
	case r.Level >= slog.LevelError:
		h.logger.Error(r.Message, args...)
	case r.Level >= slog.LevelWarn:
		h.logger.Warn(r.Message, args...)
	case r.Level >= slog.LevelInfo:
		h.logger.Info(r.Message, args...)
	default:
		h.logger.Debug(r.Message, args...)
	}
	return nil
}

func (h *hclogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &clone
}

func (h *hclogHandler) WithGroup(name string) slog.Handler {
	clone := *h
	clone.groups = append(append([]string{}, h.groups...), name)
	return &clone
}
