package pluginsdk_test

import (
	"context"
	"testing"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func TestAuthContextRoundTrip(t *testing.T) {
	ac := pluginsdk.AuthContext{
		PrincipalID: "user-1",
		TenantID:    "org:acme",
		Groups:      []string{"tenant-acme/platform-team"},
		Claims:      map[string]string{"email": "dev@acme.test"},
	}
	ctx := pluginsdk.ContextWithAuth(context.Background(), ac)
	got, ok := pluginsdk.AuthContextFrom(ctx)
	if !ok {
		t.Fatal("AuthContextFrom returned !ok")
	}
	if got.PrincipalID != "user-1" || got.TenantID != "org:acme" {
		t.Fatalf("got %+v", got)
	}
	if got.Groups[0] != "tenant-acme/platform-team" || got.Claims["email"] != "dev@acme.test" {
		t.Fatalf("got %+v", got)
	}
}

func TestAuthContextFromMissing(t *testing.T) {
	if _, ok := pluginsdk.AuthContextFrom(context.Background()); ok {
		t.Fatal("expected !ok")
	}
}

func TestAuthContextValidate(t *testing.T) {
	valid := pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid ctx rejected: %v", err)
	}
	for _, ac := range []pluginsdk.AuthContext{
		{TenantID: "t"},
		{PrincipalID: "p"},
		{},
	} {
		if err := ac.Validate(); err == nil {
			t.Fatalf("expected rejection of %+v", ac)
		}
	}
}

func TestAuthContextFromProto(t *testing.T) {
	pb := &pluginv1.AuthContext{
		PrincipalId: "p",
		TenantId:    "t",
		DisplayName: "Dev",
		Groups:      []string{"g"},
		Claims:      map[string]string{"k": "v"},
	}
	ac := pluginsdk.AuthContextFromProto(pb)
	if ac.PrincipalID != "p" || ac.TenantID != "t" || ac.DisplayName != "Dev" {
		t.Fatalf("got %+v", ac)
	}
}
