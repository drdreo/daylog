package athena

import (
	"context"
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCrashHelper(t *testing.T) {
	mode := os.Getenv("DAYLOG_CRASH_HELPER")
	if mode == "" {
		return
	}
	q, e := capture.Open()
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Defaults()
	cfg.Mode = "live"
	cfg.QuietSeconds = 0
	f := &fakeRunner{fn: func(in Input) (Output, error) {
		p := filepath.Join(q.Root, "calls")
		if _, e := os.Stat(p); e == nil {
			return Output{}, fmt.Errorf("model called again during saved-plan recovery")
		}
		if e := os.WriteFile(p, []byte("one"), 0600); e != nil {
			return Output{}, e
		}
		return Output{Version: 2, Actions: []Action{{Kind: "publish", Candidates: []string{in.Candidates[0].ID}, Type: "work", Text: "Crash-safe publication", Reason: "fixture"}}}, nil
	}}
	w := Worker{Queue: q, Config: cfg, Runner: f}
	if mode == "kill" {
		w.AfterAppend = func() error {
			if e := os.WriteFile(filepath.Join(q.Root, "appended"), []byte("ready"), 0600); e != nil {
				return e
			}
			time.Sleep(time.Minute)
			return nil
		}
	}
	if _, e := w.Once(context.Background(), false); e != nil {
		t.Fatal(e)
	}
}
func TestKilledWorkerReleasesLockAndReplaysWithoutModel(t *testing.T) {
	w, _, _ := fixture(t, "live")
	start := func(mode string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
		cmd.Env = append(os.Environ(), "DAYLOG_CRASH_HELPER="+mode)
		return cmd
	}
	cmd := start("kill")
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { _ = cmd.Process.Kill() }()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(w.Queue.Root, "appended")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			cmd.Wait()
			t.Fatal("worker never reached append/ack boundary")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	cmd.Wait()
	if b, e := start("resume").CombinedOutput(); e != nil {
		t.Fatal(string(b), e)
	}
	all, e := store.ReadAll()
	if e != nil || len(all) != 1 {
		t.Fatal(all, e)
	}
	items, e := w.Queue.List("processed")
	if e != nil || len(items) != 1 || len(items[0].Receipt.AppliedEvents) != 1 {
		t.Fatal(items, e)
	}
}
