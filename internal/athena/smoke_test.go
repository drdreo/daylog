package athena

import (
	"context"
	"github.com/drdreo/daylog/internal/capture"
	"os"
	"testing"
)

// This is the only real-inference test. It requires deliberate opt-in and uses
// solely a synthetic report in a temporary store, never personal transcripts.
func TestOptInLunaSmoke(t *testing.T) {
	if os.Getenv("DAYLOG_LUNA_SMOKE") != "1" {
		t.Skip("sanitized smoke test using the existing pi model is opt-in")
	}
	binary := os.Getenv("DAYLOG_SMOKE_PI")
	agentDir := os.Getenv("DAYLOG_SMOKE_PI_AGENT_DIR")
	if binary == "" || agentDir == "" {
		t.Fatal("set explicit DAYLOG_SMOKE_PI and DAYLOG_SMOKE_PI_AGENT_DIR")
	}
	w, _, c := fixture(t, "shadow")
	w.Config.Runner.Binary = binary
	w.Config.Runner.AgentDir = agentDir
	w.Config.Runner.Path = os.Getenv("PATH")
	in, e := w.input([]capture.Item{{Candidate: c}})
	if e != nil {
		t.Fatal(e)
	}
	out, e := (PiRunner{Config: w.Config.Runner}).Run(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	if e := Validate(in, out); e != nil {
		t.Fatal(e)
	}
}
