package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var testBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "daylog-cli-test-")
	if err != nil {
		panic(err)
	}
	testBinary = filepath.Join(dir, "daylog")
	if runtime.GOOS == "windows" {
		testBinary += ".exe"
	}
	build := exec.Command("go", "build", "-o", testBinary, "..")
	if b, e := build.CombinedOutput(); e != nil {
		fmt.Fprintln(os.Stderr, string(b), e)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func cliEnv(home, source string) []string {
	env := []string{}
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if k == "DAYLOG_DIR" || k == "DAYLOG_SOURCE" || k == "DAYLOG_INTERNAL" || k == "HOME" || k == "USERPROFILE" {
			continue
		}
		env = append(env, e)
	}
	return append(env, "HOME="+home, "USERPROFILE="+home, "DAYLOG_SOURCE="+source)
}
func runCLI(t *testing.T, home, source string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(testBinary, append([]string{"--data-dir", filepath.Join(home, "data")}, args...)...)
	cmd.Env = cliEnv(home, source)
	b, e := cmd.CombinedOutput()
	return string(b), e
}
func TestAgentSourcesCannotBypassQueue(t *testing.T) {
	home := t.TempDir()
	for _, source := range []string{"agent:pi", "agent:claude", "agent:codex"} {
		for _, kind := range []string{"work", "sidequest", "note"} {
			out, e := runCLI(t, home, source, "add", "--type", kind, "Implemented a change; validation incomplete.")
			if e != nil || !strings.HasPrefix(out, "queued ") {
				t.Fatal(out, e)
			}
		}
	}
	out, e := runCLI(t, home, "human:cli", "today", "--json")
	if e != nil {
		t.Fatal(out, e)
	}
	var day struct {
		Version int   `json:"version"`
		Entries []any `json:"entries"`
	}
	json.Unmarshal([]byte(out), &day)
	if len(day.Entries) != 0 || day.Version != 2 {
		t.Fatal(out)
	}
	if out, e := runCLI(t, home, "agent:pi", "curate", "--once"); e != nil && !strings.Contains(out, "Athena unavailable") {
		t.Fatal(out, e)
	}
}
func TestHumanCorrectionsUseEffectiveStateAndTodos(t *testing.T) {
	home := t.TempDir()
	out, e := runCLI(t, home, "human:cli", "add", "Original note")
	if e != nil {
		t.Fatal(out, e)
	}
	id := strings.Fields(out)[1]
	if out, e := runCLI(t, home, "human:cli", "amend", id, "New wording"); e != nil {
		t.Fatal(out, e)
	}
	if out, e := runCLI(t, home, "human:cli", "dismiss", "New wording", "--reason", "too-minor"); e != nil {
		t.Fatal(out, e)
	}
	out, e = runCLI(t, home, "human:cli", "today", "--json")
	if e != nil || strings.Contains(out, "New wording") {
		t.Fatal(out, e)
	}
	if out, e := runCLI(t, home, "human:cli", "restore", id); e != nil {
		t.Fatal(out, e)
	}
	out, e = runCLI(t, home, "agent:codex", "add", "--type", "todo", "Investigate retry ownership")
	if e != nil || !strings.HasPrefix(out, "proposed ") {
		t.Fatal(out, e)
	}
	todo := strings.Fields(out)[1]
	if _, e := runCLI(t, home, "agent:codex", "accept", todo); e == nil {
		t.Fatal("agent accepted obligation")
	}
	if _, e := runCLI(t, home, "human:cli", "done", todo); e == nil {
		t.Fatal("unadopted todo completed")
	}
	for _, verb := range []string{"accept", "done"} {
		if out, e := runCLI(t, home, "human:cli", verb, todo); e != nil {
			t.Fatal(out, e)
		}
	}
	out, e = runCLI(t, home, "human:cli", "today", "--json")
	if e != nil || !strings.Contains(out, `"display_at"`) || !strings.Contains(out, `"done": true`) || strings.Contains(out, `"ts"`) {
		t.Fatal(out, e)
	}
	if _, e := runCLI(t, home, "agent:pi", "reopen", todo); e == nil {
		t.Fatal("agent reopened obligation")
	}
	if out, e := runCLI(t, home, "human:widget", "reopen", todo); e != nil {
		t.Fatal(out, e)
	}
	out, e = runCLI(t, home, "human:cli", "today", "--json")
	if e != nil || strings.Contains(out, `"done": true`) || !strings.Contains(out, `"verdict": "accepted"`) {
		t.Fatal(out, e)
	}
	if _, e := runCLI(t, home, "human:cli", "reopen", todo); e == nil {
		t.Fatal("open todo reopened")
	}
	if out, e := runCLI(t, home, "human:cli", "done", todo); e != nil {
		t.Fatal(out, e)
	}
}
func TestConcurrentProducerProcesses(t *testing.T) {
	home := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out, e := runCLI(t, home, "agent:pi", "add", "--type", "work", "--idempotency-key", fmt.Sprint(i%4), "Same factual report")
			if e != nil || !strings.HasPrefix(out, "queued ") {
				t.Error(out, e)
			}
		}(i)
	}
	wg.Wait()
	out, e := runCLI(t, home, "human:cli", "queue", "list", "--status", "pending")
	if e != nil {
		t.Fatal(out, e)
	}
	var items []any
	if json.Unmarshal([]byte(out), &items) != nil || len(items) != 4 {
		t.Fatal(out)
	}
}
func TestScratchSetupDoesNotTouchRealConfig(t *testing.T) {
	home := t.TempDir()
	out, e := runCLI(t, home, "human:cli", "init")
	if e != nil {
		t.Fatal(out, e)
	}
	out, e = runCLI(t, home, "human:cli", "setup", "--approve-project", filepath.Join(home, "project"), "--capture-scope", "codex="+filepath.Join(home, "native"), "--install-adapters", "--schedule")
	if e != nil {
		t.Fatal(out, e)
	}
	b, e := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if e != nil || !strings.Contains(string(b), "--data-dir") {
		t.Fatal(string(b), e)
	}
	if _, e := os.Stat(filepath.Join(home, "data", "events")); !os.IsNotExist(e) {
		t.Fatal("setup published events")
	}
}
func TestNativeHookNeutralJSON(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(home, "project")
	os.MkdirAll(project, 0700)
	if out, e := runCLI(t, home, "human:cli", "setup", "--approve-project", project, "--capture-scope", "codex="+filepath.Join(home, "native")); e != nil {
		t.Fatal(out, e)
	}
	payload, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "session_id": "s", "turn_id": "t", "cwd": project, "last_assistant_message": "Implemented bounded retries."})
	cmd := exec.Command(testBinary, "--data-dir", filepath.Join(home, "data"), "capture", "--adapter", "codex", "--harness-version", "0.153.2")
	cmd.Env = cliEnv(home, "agent:codex")
	cmd.Stdin = strings.NewReader(string(payload))
	b, e := cmd.CombinedOutput()
	if e != nil || strings.TrimSpace(string(b)) != "{}" {
		t.Fatal(string(b), e)
	}
}
