package pluginsdk_test

import (
	"testing"

	pluginv1 "github.com/7K-Inari/inari-api/gen/go/inari/plugin/v1"
	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
)

func TestValidateAuthMethods(t *testing.T) {
	cases := []struct {
		name    string
		methods []pluginsdk.AuthMethod
		wantErr bool
	}{
		{name: "empty is valid (host default oidc-user applies)"},
		{
			name: "single oidc-user with audience and scopes",
			methods: []pluginsdk.AuthMethod{{
				Type:      pluginsdk.AuthMethodOIDCUser,
				Audience:  "https://git.example.com",
				Scopes:    []string{"read", "write"},
				IsDefault: true,
			}},
		},
		{
			name: "all five types",
			methods: []pluginsdk.AuthMethod{
				{Type: pluginsdk.AuthMethodOIDCUser, IsDefault: true},
				{Type: pluginsdk.AuthMethodServiceAccount},
				{Type: pluginsdk.AuthMethodAPIKey},
				{Type: pluginsdk.AuthMethodSharedSecret},
				{Type: pluginsdk.AuthMethodOIDCSSOSession},
			},
		},
		{
			name:    "unspecified type rejected",
			methods: []pluginsdk.AuthMethod{{}},
			wantErr: true,
		},
		{
			name:    "multiple defaults rejected",
			methods: []pluginsdk.AuthMethod{{Type: pluginsdk.AuthMethodOIDCUser, IsDefault: true}, {Type: pluginsdk.AuthMethodAPIKey, IsDefault: true}},
			wantErr: true,
		},
		{
			name:    "duplicate type rejected",
			methods: []pluginsdk.AuthMethod{{Type: pluginsdk.AuthMethodAPIKey}, {Type: pluginsdk.AuthMethodAPIKey}},
			wantErr: true,
		},
		{
			name:    "audience on non-OIDC type rejected",
			methods: []pluginsdk.AuthMethod{{Type: pluginsdk.AuthMethodAPIKey, Audience: "x"}},
			wantErr: true,
		},
		{
			name:    "scopes on non-OIDC type rejected",
			methods: []pluginsdk.AuthMethod{{Type: pluginsdk.AuthMethodSharedSecret, Scopes: []string{"read"}}},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := pluginsdk.ValidateAuthMethods(tc.methods)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestAuthMethodProtoRoundTrip(t *testing.T) {
	in := pluginsdk.AuthMethod{
		Type:      pluginsdk.AuthMethodOIDCUser,
		Audience:  "https://git.example.com",
		Scopes:    []string{"read"},
		IsDefault: true,
	}
	pb := in.Proto()
	if pb.GetType() != pluginv1.AuthMethod_TYPE_OIDC_USER {
		t.Fatalf("type = %v", pb.GetType())
	}
	if pb.GetAudience() != in.Audience || pb.GetIsDefault() != in.IsDefault || len(pb.GetScopes()) != 1 {
		t.Fatalf("proto = %+v", pb)
	}
	out := pluginsdk.AuthMethodFromProto(pb)
	if out.Type != in.Type || out.Audience != in.Audience || out.IsDefault != in.IsDefault ||
		len(out.Scopes) != 1 || out.Scopes[0] != "read" {
		t.Fatalf("round trip = %+v, want %+v", out, in)
	}
}

func TestAuthMethodTypeWireNames(t *testing.T) {
	cases := map[pluginsdk.AuthMethodType]string{
		pluginsdk.AuthMethodOIDCUser:       "oidc-user",
		pluginsdk.AuthMethodServiceAccount: "service-account",
		pluginsdk.AuthMethodAPIKey:         "api-key",
		pluginsdk.AuthMethodSharedSecret:   "shared-secret",
		pluginsdk.AuthMethodOIDCSSOSession: "oidc-sso-session",
	}
	for typ, want := range cases {
		if got := typ.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
		parsed, ok := pluginsdk.ParseAuthMethodType(want)
		if !ok || parsed != typ {
			t.Errorf("ParseAuthMethodType(%q) = %v, %v", want, parsed, ok)
		}
	}
	if _, ok := pluginsdk.ParseAuthMethodType("bogus"); ok {
		t.Error("ParseAuthMethodType(bogus) should not be ok")
	}
}
