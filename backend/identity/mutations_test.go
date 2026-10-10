// identity/mutations_test.go

package identity

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMutationLockAcrossProcesses(t *testing.T) {
	admin, err := NewAdmin("http://127.0.0.1:4834", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("GOPTIVUM_LOCK_CHILD") == "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		unlock, err := admin.lockChanges(ctx)
		if unlock != nil {
			unlock()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("child acquired the parent's lock: %v", err)
		}
		return
	}
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	unlock, err := admin.lockChanges(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMutationLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "GOPTIVUM_LOCK_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v: %s", err, output)
	}
	t.Run("password recovery script", func(t *testing.T) {
		python, err := exec.LookPath("python3")
		if err != nil {
			t.Skip("python3 is absent")
		}
		if err := exec.CommandContext(t.Context(), python, "-c", "import yaml").Run(); err != nil {
			t.Skip("the recovery script's PyYAML dependency is absent")
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, python, "-c", `import runpy
script = runpy.run_path("../../scripts/set-password.py")
print("ready", flush=True)
with script["mutation_lock"]("http://127.0.0.1:4834"):
    print("acquired", flush=True)
`)
		output, err := cmd.CombinedOutput()
		if ctx.Err() != context.DeadlineExceeded || err == nil || !strings.Contains(string(output), "ready") || strings.Contains(string(output), "acquired") {
			t.Fatalf("Python writer did not wait for the Go lock: %v: %s", err, output)
		}
	})
}
