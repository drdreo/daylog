package athena

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/drdreo/daylog/internal/capture"
)

// Real inference is opt-in and uses synthetic reports in temporary stores only.
// Check dispositions automatically and log wording for human review: matching a
// disposition alone does not establish that a summary preserves every nuance.
func TestOptInLunaSmoke(t *testing.T) {
	if os.Getenv("DAYLOG_LUNA_SMOKE") != "1" {
		t.Skip("synthetic policy checks using the existing pi model are opt-in")
	}
	binary := os.Getenv("DAYLOG_SMOKE_PI")
	agentDir := os.Getenv("DAYLOG_SMOKE_PI_AGENT_DIR")
	if binary == "" || agentDir == "" {
		t.Fatal("set explicit DAYLOG_SMOKE_PI and DAYLOG_SMOKE_PI_AGENT_DIR")
	}
	cases := []struct {
		name, report, want string
	}{
		{"report-without-proof", "Implemented name-based campaign collections with rename and deletion recovery. Older briefs remain discoverable through the fallback.", "publish"},
		{"partial-result", "Implemented the campaign collection picker. Rename/deletion recovery tests pass locally. The UI has not been exercised and deployment has not been attempted.", "publish"},
		{"attempted-fix", "Reproduced lost writes when a save overlaps a refresh. Added a mutex as an attempted fix, but the reproduction still fails. Narrowed the remaining race to cache invalidation; the bug is not fixed.", "publish"},
		{"routine-noise", "Retried the same unchanged test command; it passed again. No code changes, new findings, or decisions.", "skip"},
		{"unclear-result", "The rollout may or may not be complete; I don't know what actually happened. I have no concrete change or finding to report.", "hold"},
		{"contradictory-result", "The release is deployed and serving every customer. The release is also not deployed and serves no customers. These statements refer to the same release and time; I cannot resolve which is true.", "hold"},
		{"narrow-overclaim", "All customer data is now safe. Specifically, I added retries for failed backup uploads; retry tests pass locally. Restoring a backup remains untested, and I have not assessed other data-loss risks.", "publish"},
		{"detailed-handover", "Updated the reporting instructions to include changes, checks, and limitations. Replaced custom authentication code with a normal command invocation and persistent preview runs with non-consuming dry runs. Added tests for failed fixes, incomplete work, routine noise, unclear results, contradictions, and exaggerated claims. Local tests and race checks pass; desktop decoding checks pass too. Published a sample backlog and observed the scheduled worker recover a rejected reference. Linux and Windows builds succeed, but native runtime behavior and hook capture remain untested.", "publish"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, _, c := fixture(t, "live")
			w.Config.Runner.Binary = binary
			w.Config.Runner.AgentDir = agentDir
			w.Config.Runner.Path = os.Getenv("PATH")
			c.Text = tc.report
			c.Terminal = "unknown"
			if tc.name == "partial-result" {
				c.Refs = []string{"gh:pr:github.com/o/r#142", "linear:ABC-7"}
			}
			in, err := w.input([]capture.Item{{Candidate: c}})
			if err != nil {
				t.Fatal(err)
			}
			out, err := (PiRunner{Config: w.Config.Runner}).Run(context.Background(), in)
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate(in, out); err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(out)
			t.Log(string(b))
			for _, action := range out.Actions {
				if tc.name == "partial-result" && len(action.Refs) == 0 {
					t.Error("lost the implementation's supplied reference links")
				}
				if action.Kind != tc.want {
					t.Errorf("want %s, got %s: %s", tc.want, action.Kind, action.Reason)
				}
			}
		})
	}
}
