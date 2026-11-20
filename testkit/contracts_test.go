package testkit_test

import (
	"context"
	"strings"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/testkit"
)

func TestBackendContractAssertion(t *testing.T) {
	client := testkit.Run(t, pluginsdk.New(pluginsdk.Info{Name: "contracts", Version: "1.0.0"}))
	ctx := context.Background()
	if err := testkit.CheckBackendContract(ctx, client, "inari.plugin.v1"); err != nil {
		t.Fatalf("v1 contract rejected: %v", err)
	}
	if err := testkit.CheckBackendContract(ctx, client, "inari.plugin.v2", "inari.plugin.v1"); err != nil {
		t.Fatalf("mixed expected set rejected: %v", err)
	}
	if err := testkit.CheckBackendContract(ctx, client, "inari.plugin.v2"); err == nil || !strings.Contains(err.Error(), "inari.plugin.v1") {
		t.Fatalf("mismatch error = %v", err)
	}
	if err := testkit.CheckBackendContract(ctx, client); err == nil {
		t.Fatal("empty expected contract set accepted")
	}
	testkit.RequireBackendContract(t, ctx, client, "inari.plugin.v1")
}
