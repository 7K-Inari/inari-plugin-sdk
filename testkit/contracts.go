package testkit

import (
	"context"
	"fmt"
	"strings"
	"testing"

	extensionv1 "github.com/7K-Inari/inari-api/contract/extension/v1"
)

// CheckBackendContract verifies the plugin's reported contract against an
// expected non-empty set.
func CheckBackendContract(ctx context.Context, client *Client, expected ...string) error {
	if len(expected) == 0 {
		return fmt.Errorf("at least one expected backend contract is required")
	}
	if client == nil {
		return fmt.Errorf("plugin client is required")
	}
	info, err := client.Info(ctx)
	if err != nil {
		return fmt.Errorf("get plugin info: %w", err)
	}
	if info == nil {
		return fmt.Errorf("plugin returned empty info")
	}
	actual := info.GetApiVersion()
	if !extensionv1.Intersects([]string{actual}, expected) {
		return fmt.Errorf("plugin reports contract %q, expected one of [%s]", actual, strings.Join(expected, ", "))
	}
	return nil
}

// RequireBackendContract is the fatal assertion form of CheckBackendContract.
func RequireBackendContract(t testing.TB, ctx context.Context, client *Client, expected ...string) {
	t.Helper()
	if err := CheckBackendContract(ctx, client, expected...); err != nil {
		t.Fatal(err)
	}
}
