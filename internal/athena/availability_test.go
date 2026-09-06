package athena

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/drdreo/daylog/internal/config"
)

func TestUnavailableExistingHarnessOrModelHasActionableError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake harness")
	}
	for _, failure := range []string{"missing-harness", "missing-auth", "missing-model"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			cfg := config.Defaults().Runner
			cfg.Binary = filepath.Join(root, "pi")
			cfg.AgentDir = filepath.Join(root, "existing-pi")
			cfg.Path = os.Getenv("PATH")
			cfg.TimeoutSeconds = 10
			if err := os.Mkdir(cfg.AgentDir, 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("TEST_EXISTING_PI_DIR", cfg.AgentDir)
			// Resolve credentials only through the user's configured harness directory.
			// Provider stderr must not leak credentials into the diagnostic.
			script := "#!/bin/sh\nif [ \"$1\" = auth ]; then\n [ \"$PI_CODING_AGENT_DIR\" = \"$TEST_EXISTING_PI_DIR\" ] || exit 91\n"
			if failure == "missing-auth" {
				script += " echo PRIVATE-CREDENTIAL >&2; exit 1\n"
			} else {
				script += " echo existing-token; exit 0\n"
			}
			script += "fi\necho PRIVATE-CREDENTIAL >&2\nexit 1\n"
			if failure != "missing-harness" {
				if err := os.WriteFile(cfg.Binary, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := (PiRunner{Config: cfg}).Run(context.Background(), Input{Version: 2})
			if err == nil {
				t.Fatal("unavailable Athena model succeeded")
			}
			for _, want := range []string{"Athena", cfg.Provider + "/" + cfg.Model, "daylog doctor --check-model", "reports remain queued"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("missing %q in %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "PRIVATE-CREDENTIAL") {
				t.Fatal("provider stderr leaked")
			}
		})
	}
}
