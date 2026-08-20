package pluginsdk_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func TestNewLoggerBridgesToStderr(t *testing.T) {
	var buf bytes.Buffer
	logger := pluginsdk.NewLogger("myplugin", pluginsdk.WithLogOutput(&buf))
	logger.Info("hello", slog.String("action", "greet"), slog.Int("attempt", 2))
	out := buf.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "action=greet") || !strings.Contains(out, "attempt=2") {
		t.Fatalf("output = %q", out)
	}
}

func TestNewLoggerLevelMapping(t *testing.T) {
	var buf bytes.Buffer
	logger := pluginsdk.NewLogger("myplugin", pluginsdk.WithLogOutput(&buf), pluginsdk.WithLogLevel(slog.LevelWarn))
	logger.Debug("hidden")
	logger.Warn("shown")
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatal("debug message should be filtered at warn level")
	}
	if !strings.Contains(out, "shown") {
		t.Fatal("warn message missing")
	}
	if !strings.Contains(out, "WARN") && !strings.Contains(out, "warn") {
		t.Fatalf("level label missing in %q", out)
	}
}

func TestNewLoggerErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := pluginsdk.NewLogger("myplugin", pluginsdk.WithLogOutput(&buf))
	logger.ErrorContext(context.Background(), "failed", slog.String("err", "boom"))
	if !strings.Contains(buf.String(), "failed") || !strings.Contains(buf.String(), "err=boom") {
		t.Fatalf("output = %q", buf.String())
	}
}
