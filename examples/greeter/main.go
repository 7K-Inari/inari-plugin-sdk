// Command greeter is the minimal Inari backend extension example: it declares
// a single "greet" action that echoes the authenticated principal and tenant
// forwarded by the control plane, plus a "crash" action used to demonstrate
// host-side crash isolation.
package main

import (
	"context"
	"fmt"
	"os"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func main() {
	log := pluginsdk.NewLogger("greeter")

	p := pluginsdk.New(pluginsdk.Info{Name: "greeter", Version: "0.1.0"})

	err := p.RegisterAction(pluginsdk.Action{
		Name:        "greet",
		Description: "Greets the authenticated caller within their tenant.",
		Handler: func(ctx context.Context, req *pluginsdk.Request) (*pluginsdk.Response, error) {
			ac, ok := pluginsdk.AuthContextFrom(ctx)
			if !ok {
				return nil, pluginsdk.Errorf(pluginsdk.CodeUnauthenticated, "no auth context")
			}
			log.Info("greet invoked", "principal", ac.PrincipalID, "tenant", ac.TenantID, "request_id", req.RequestID)
			msg := fmt.Sprintf("hello %s from tenant %s", ac.PrincipalID, ac.TenantID)
			return &pluginsdk.Response{Result: []byte(fmt.Sprintf(`{"message":%q}`, msg))}, nil
		},
	})
	if err != nil {
		log.Error("register greet", "err", err)
		os.Exit(1)
	}

	err = p.RegisterAction(pluginsdk.Action{
		Name:        "crash",
		Description: "Exits the process immediately (crash-isolation demo).",
		Handler: func(context.Context, *pluginsdk.Request) (*pluginsdk.Response, error) {
			os.Exit(2)
			return nil, nil
		},
	})
	if err != nil {
		log.Error("register crash", "err", err)
		os.Exit(1)
	}

	if err := p.Serve(context.Background()); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
