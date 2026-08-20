package pluginsdk_test

import (
	"context"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func okHandler(_ context.Context, req *pluginsdk.Request) (*pluginsdk.Response, error) {
	return &pluginsdk.Response{Result: req.Payload}, nil
}

func TestRegisterActionAndList(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "test", Version: "0.1.0"})
	err := p.RegisterAction(pluginsdk.Action{
		Name:        "greet",
		Description: "says hi",
		Handler:     okHandler,
	})
	if err != nil {
		t.Fatalf("RegisterAction: %v", err)
	}
	actions := p.Actions()
	if len(actions) != 1 || actions[0].Name != "greet" {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestRegisterActionRejectsDuplicates(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "test", Version: "0.1.0"})
	a := pluginsdk.Action{Name: "greet", Handler: okHandler}
	if err := p.RegisterAction(a); err != nil {
		t.Fatal(err)
	}
	if err := p.RegisterAction(a); err == nil {
		t.Fatal("expected duplicate registration error")
	}
}

func TestRegisterActionRejectsInvalid(t *testing.T) {
	p := pluginsdk.New(pluginsdk.Info{Name: "test", Version: "0.1.0"})
	if err := p.RegisterAction(pluginsdk.Action{Handler: okHandler}); err == nil {
		t.Fatal("expected error for empty name")
	}
	if err := p.RegisterAction(pluginsdk.Action{Name: "x"}); err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestHandshakeDefaults(t *testing.T) {
	h := pluginsdk.Handshake()
	if h.ProtocolVersion != pluginsdk.ProtocolVersion {
		t.Fatalf("version = %d", h.ProtocolVersion)
	}
	if h.MagicCookieKey == "" || h.MagicCookieValue == "" {
		t.Fatal("magic cookie must be set")
	}
}
