package host_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	pluginsdk "github.com/7K-Inari/inari-plugin-sdk"
	"github.com/7K-Inari/inari-plugin-sdk/host"
)

// buildGreeter compiles the example plugin into a temp binary.
func buildGreeter(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "greeter")
	out, err := exec.Command("go", "build", "-o", bin, "../examples/greeter").CombinedOutput()
	if err != nil {
		t.Fatalf("build greeter: %v\n%s", err, out)
	}
	return bin
}

func launchGreeter(t *testing.T) (*host.Client, context.Context) {
	t.Helper()
	ctx := context.Background()
	c, err := host.Launch(ctx, buildGreeter(t))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	t.Cleanup(c.Close)
	return c, ctx
}

func TestLaunchHandshakeAndGetInfo(t *testing.T) {
	c, ctx := launchGreeter(t)
	info, err := c.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.GetName() != "greeter" || info.GetApiVersion() != "inari.plugin.v1" {
		t.Fatalf("info = %+v", info)
	}
}

func TestCapabilitiesAndInvokeWithAuthContext(t *testing.T) {
	c, ctx := launchGreeter(t)

	actions, err := c.Capabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, a := range actions {
		names[a.GetName()] = true
	}
	if !names["greet"] {
		t.Fatalf("greet not advertised: %v", names)
	}

	resp, err := c.Invoke(ctx, "greet", pluginsdk.AuthContext{
		PrincipalID: "user-42",
		TenantID:    "org:acme",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"message":"hello user-42 from tenant org:acme"}`
	if string(resp.Result) != want {
		t.Fatalf("result = %s, want %s", resp.Result, want)
	}
}

func TestInvokeWithoutAuthContextFailsClosed(t *testing.T) {
	c, ctx := launchGreeter(t)
	_, err := c.Invoke(ctx, "greet", pluginsdk.AuthContext{}, nil)
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *pluginsdk.Error", err)
	}
	if pe.Code != pluginsdk.CodeUnauthenticated {
		t.Fatalf("code = %v, want UNAUTHENTICATED", pe.Code)
	}
}

func TestChecksumMismatchRefusesLaunch(t *testing.T) {
	bin := buildGreeter(t)
	sum, err := host.Checksum(bin)
	if err != nil {
		t.Fatal(err)
	}
	// Tamper with the binary after pinning its checksum.
	f, err := os.OpenFile(bin, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("tampered"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = host.Launch(context.Background(), bin, host.WithChecksum(sum))
	if err == nil {
		t.Fatal("expected launch refusal on checksum mismatch")
	}
}

func TestCrashIsolation(t *testing.T) {
	c, ctx := launchGreeter(t)
	_, err := c.Invoke(ctx, "crash", pluginsdk.AuthContext{PrincipalID: "p", TenantID: "t"}, nil)
	if !errors.Is(err, host.ErrPluginCrashed) {
		t.Fatalf("err = %v, want ErrPluginCrashed", err)
	}
	// The supervising host observes the exit.
	if !c.Exited() {
		t.Fatal("host did not observe plugin exit")
	}
}

func TestMagicCookieMismatch(t *testing.T) {
	bin := buildGreeter(t)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "INARI_PLUGIN_MAGIC_COOKIE=wrong")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected startup refusal, output: %s", out)
	}
	if len(out) == 0 {
		t.Fatal("expected handshake error output")
	}
}

func TestHealth(t *testing.T) {
	c, ctx := launchGreeter(t)
	serving, _, err := c.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !serving {
		t.Fatal("greeter should report serving")
	}
}
