// This file demonstrates how a third-party plugin author tests a plugin using
// only the public SDK and testkit APIs — no internal imports.
package testkit_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/testkit"
)

// thirdPartyPlugin is a stand-in for a plugin author's own code.
func thirdPartyPlugin(t *testing.T) *pluginsdk.Plugin {
	t.Helper()
	p := pluginsdk.New(pluginsdk.Info{Name: "upper", Version: "1.0.0"})
	err := p.RegisterAction(pluginsdk.Action{
		Name:        "shout",
		Description: "Greets loudly.",
		Handler: func(ctx context.Context, req *pluginsdk.Request) (*pluginsdk.Response, error) {
			ac, ok := pluginsdk.AuthContextFrom(ctx)
			if !ok {
				return nil, pluginsdk.Errorf(pluginsdk.CodeUnauthenticated, "who are you?")
			}
			return &pluginsdk.Response{
				Result: []byte(fmt.Sprintf(`{"shout":"HELLO %s OF %s!"}`, ac.PrincipalID, ac.TenantID)),
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestThirdPartyPlugin(t *testing.T) {
	ctx := context.Background()
	client := testkit.Run(t, thirdPartyPlugin(t))

	info, err := client.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.GetName() != "upper" || info.GetApiVersion() != "inari.plugin.v1" {
		t.Fatalf("info = %+v", info)
	}

	actions, err := client.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].GetName() != "shout" {
		t.Fatalf("actions = %+v", actions)
	}

	resp, err := client.Invoke(ctx, "shout", pluginsdk.AuthContext{
		PrincipalID: "dev-7",
		TenantID:    "org:globex",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"shout":"HELLO dev-7 OF org:globex!"}`
	if string(resp.Result) != want {
		t.Fatalf("result = %s, want %s", resp.Result, want)
	}

	// Fail-closed: no identity, no service.
	_, err = client.Invoke(ctx, "shout", pluginsdk.AuthContext{}, nil)
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != pluginsdk.CodeUnauthenticated {
		t.Fatalf("err = %v, want UNAUTHENTICATED", err)
	}

	serving, _, err := client.Health(ctx)
	if err != nil || !serving {
		t.Fatalf("health: serving=%v err=%v", serving, err)
	}
}
