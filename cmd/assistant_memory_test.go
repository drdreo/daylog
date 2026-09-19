package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/drdreo/daylog/internal/memory"
)

// Every command gets a fresh Cobra tree and an explicit synthetic temporary root.
func executeMemoryCLI(ctx context.Context, input string, args ...string) (string, error) {
	c := newAthenaCommand()
	c.SilenceUsage, c.SilenceErrors = true, true
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&out)
	c.SetIn(strings.NewReader(input))
	c.SetArgs(args)
	err := c.ExecuteContext(ctx)
	return out.String(), err
}

func runMemoryCLI(t *testing.T, root, input string, args ...string) (string, error) {
	t.Helper()
	return executeMemoryCLI(context.Background(), input, append(args, "--root", root)...)
}

func syntheticMemory(id, owner, kind, status string) memory.Input {
	return memory.Input{
		ID: id, Owner: owner, Author: owner, Subject: owner,
		Kind: kind, Status: status, Content: "Synthetic violet lantern observation",
		Context: "test-room", Project: "synthetic-project", Topic: "synthetic-topic",
		OccurredAt: "2026-01-02T03:04:05Z", Provenance: "synthetic CLI fixture; no external source",
	}
}

func memoryJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func recordSyntheticMemory(t *testing.T, root string, in memory.Input) memory.Record {
	t.Helper()
	out, err := runMemoryCLI(t, root, memoryJSON(t, in), "memory", "record", "--owner", in.Owner, "--file", "-")
	if err != nil {
		t.Fatal(out, err)
	}
	var record memory.Record
	if err := json.Unmarshal([]byte(out), &record); err != nil {
		t.Fatal(out, err)
	}
	return record
}

func assertMemoryRootAbsent(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected store initialization: entries=%v error=%v", entries, err)
	}
}

func TestMemoryReadCommandsNeverInitialize(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "agent:synthetic")
	for _, args := range [][]string{
		{"memory", "list", "--owner", "athena"},
		{"memory", "recall", "violet", "--owner", "dreo"},
		{"memory", "export", "--owners", "athena,dreo"},
		{"memory", "show", "synthetic", "--owner", "athena"},
		{"memory", "show", "synthetic", "--owner", "athena", "--history"},
		{"dreams"}, {"dreams", "synthetic"}, {"dreams", "--owners", "athena,dreo"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			parent := t.TempDir()
			if out, err := runMemoryCLI(t, filepath.Join(parent, "memory"), "", args...); err == nil {
				t.Fatal("missing store read succeeded", out)
			}
			assertMemoryRootAbsent(t, parent)
		})
	}
}

func TestMemoryRequiresExplicitAbsoluteRoot(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	parent := t.TempDir()
	t.Setenv("DAYLOG_DIR", filepath.Join(parent, "environment-must-not-be-used"))
	for _, rootArgs := range [][]string{nil, {"--root", "relative-memory"}, {"--root", ""}} {
		args := append([]string{"memory", "rebuild"}, rootArgs...)
		out, err := executeMemoryCLI(context.Background(), "", args...)
		if err == nil || !strings.Contains(err.Error(), "absolute") {
			t.Fatal(out, err)
		}
	}
	assertMemoryRootAbsent(t, parent)
}

func TestMemoryCancellationBeforeOpen(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	input := memoryJSON(t, syntheticMemory("cancelled", "athena", "episode", "reported"))
	for _, args := range [][]string{
		{"memory", "record", "--owner", "athena", "--file", "-"},
		{"memory", "list", "--owner", "athena"},
		{"memory", "rebuild"}, {"dreams"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			parent := t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			out, err := executeMemoryCLI(ctx, input, append(args, "--root", filepath.Join(parent, "memory"))...)
			if !errors.Is(err, context.Canceled) || out != "" {
				t.Fatalf("output=%q error=%v", out, err)
			}
			assertMemoryRootAbsent(t, parent)
		})
	}
}

func TestMemoryPendingPipeInputCancellation(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint("deadline=", deadline), func(t *testing.T) {
			parent := t.TempDir()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			defer writer.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			want := context.DeadlineExceeded
			if !deadline {
				cancel()
				ctx, cancel = context.WithCancel(context.Background())
				timer := time.AfterFunc(40*time.Millisecond, cancel)
				defer timer.Stop()
				want = context.Canceled
			}
			defer cancel()
			c := newAthenaCommand()
			c.SilenceUsage, c.SilenceErrors = true, true
			c.SetOut(io.Discard)
			c.SetErr(io.Discard)
			c.SetIn(reader)
			c.SetArgs([]string{"memory", "record", "--root", filepath.Join(parent, "memory"), "--owner", "athena", "--file", "-"})
			result := make(chan error, 1)
			go func() { result <- c.ExecuteContext(ctx) }()
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatalf("expected %v, got %v", want, err)
				}
			case <-time.After(2 * time.Second):
				// Close the writer to unblock a regressed implementation and join
				// the test goroutine rather than leaving it behind.
				_ = writer.Close()
				<-result
				t.Fatal("pending input ignored cancellation")
			}
			assertMemoryRootAbsent(t, parent)
			// Cancellation must not close caller-owned stdin or leave a stale
			// deadline that breaks a subsequent caller read.
			if _, err := writer.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
			var marker [1]byte
			if _, err := io.ReadFull(reader, marker[:]); err != nil || marker[0] != 'x' {
				t.Fatalf("stdin was closed or its deadline was not cleared: %v", err)
			}
		})
	}
}

func TestMemoryInheritedPipeRestoresInput(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("inherited blocking-pipe adaptation is supported on macOS and Linux")
	}
	if os.Getenv("DAYLOG_MEMORY_INPUT_CHILD") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		c := newAthenaCommand()
		c.SetContext(ctx)
		c.SetIn(os.Stdin)
		if _, err := readMemoryInput(c, "-"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("pending inherited stdin: %v", err)
		}
		fmt.Println("input-cancelled")
		// The parent delays this byte until we have returned from the bounded
		// read. A closed original fd fails; leaked O_NONBLOCK returns EAGAIN.
		var marker [1]byte
		if _, err := io.ReadFull(os.Stdin, marker[:]); err != nil || marker[0] != 'x' {
			t.Fatalf("original stdin not restored: %v", err)
		}
		return
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	// Fd makes this descriptor blocking, like stdin inherited from a shell.
	_ = reader.Fd()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMemoryInheritedPipeRestoresInput$")
	child.Env = append(os.Environ(), "DAYLOG_MEMORY_INPUT_CHILD=1")
	child.Stdin = reader
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	output := bufio.NewReader(stdout)
	line, readErr := output.ReadString('\n')
	if readErr != nil || line != "input-cancelled\n" {
		cancel()
		_ = child.Wait()
		t.Fatalf("child did not cancel pending input: %q, %v, %s", line, readErr, stderr.String())
	}
	time.Sleep(40 * time.Millisecond)
	_, writeErr := writer.Write([]byte("x"))
	_ = writer.Close()
	remainder, _ := io.ReadAll(output)
	if err := child.Wait(); err != nil || writeErr != nil {
		t.Fatalf("inherited stdin restoration failed: %v, write=%v, output=%s, stderr=%s", err, writeErr, remainder, stderr.String())
	}
}

type memoryUnsupportedInput struct{ read bool }

func (r *memoryUnsupportedInput) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("unsupported reader must not be called")
}

func TestMemoryRejectsUnsupportedReaderWithoutReading(t *testing.T) {
	reader := &memoryUnsupportedInput{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := readMemoryBytes(ctx, reader); err == nil || !strings.Contains(err.Error(), "unsupported") || reader.read {
		t.Fatalf("generic reader was not rejected before reading: read=%v error=%v", reader.read, err)
	}
}

func TestMemoryRejectsNonregularInputFile(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	parent := t.TempDir()
	root := filepath.Join(parent, "memory")
	inputDir := t.TempDir()
	if out, err := runMemoryCLI(t, root, "", "memory", "record", "--owner", "athena", "--file", inputDir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatal(out, err)
	}
	assertMemoryRootAbsent(t, parent)
	if runtime.GOOS == "windows" {
		t.Skip("filesystem FIFOs are not supported on Windows")
	}
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo is not available")
	}
	fifo := filepath.Join(inputDir, "input.fifo")
	if out, err := exec.Command(mkfifo, fifo).CombinedOutput(); err != nil {
		t.Fatalf("create synthetic FIFO: %s: %v", out, err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := runMemoryCLI(t, root, "", "memory", "record", "--owner", "athena", "--file", fifo)
		result <- err
	}()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("FIFO not rejected: %v", err)
		}
	case <-time.After(2 * time.Second):
		// Unblock an erroneous blocking open and join the test goroutine.
		f, err := os.OpenFile(fifo, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		<-result
		t.Fatal("FIFO blocked instead of being rejected before open")
	}
	assertMemoryRootAbsent(t, parent)
}

func TestMemoryInvalidFlagsAndScopes(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	for _, args := range [][]string{
		{"memory", "list"},
		{"memory", "list", "--owner", "athena", "--owners", "athena,dreo"},
		{"memory", "list", "--owner", "other"},
		{"memory", "list", "--owners", ""},
		{"memory", "list", "--owners", "athena,athena"},
		{"memory", "list", "--owners", "athena, dreo"},
		{"memory", "list", "--owner", "athena", "--limit", "0"},
		{"memory", "list", "--owner", "athena", "--limit", "101"},
		{"memory", "list", "--owners", "athena,dreo", "--after", "previous"},
		{"memory", "list", "--owner", "athena", "--after", "../bad"},
		{"memory", "recall", "", "--owner", "athena"},
		{"memory", "recall", "  ", "--owner", "athena"},
		{"memory", "recall", "term", "--owner", "athena", "--after", "previous"},
		{"memory", "show", "id", "--owner", "athena", "--owners", "dreo"},
		{"memory", "show", "../bad", "--owner", "athena"},
		{"memory", "record", "--owner", "athena"},
		{"memory", "correct", "id", "--owner", "athena", "--file", "-"},
		{"memory", "correct", "id", "--owner", "athena", "--revision", "-1", "--file", "-"},
		{"memory", "forget", "id", "--owner", "athena", "--revision", "1"},
		{"memory", "forget", "id", "--owner", "athena", "--revision", "0", "--confirm"},
		{"dreams", "--owners", "dreo"},
		{"dreams", "id", "--limit", "10"},
		{"dreams", "id", "--status", "speculative"},
		{"dreams", "--owner", "dreo"},
		{"dreams", "--kind", "episode"},
		{"dreams", "--file", "-"},
		{"dreams", "--confirm"},
		{"dreams", "--generate"},
		{"dreams", "record", "id"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			parent := t.TempDir()
			if out, err := runMemoryCLI(t, filepath.Join(parent, "memory"), "{}", args...); err == nil {
				t.Fatal("invalid command succeeded", out)
			}
			assertMemoryRootAbsent(t, parent)
		})
	}
}

func TestMemoryStrictBoundedInput(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	valid := memoryJSON(t, syntheticMemory("input", "athena", "episode", "reported"))
	for name, input := range map[string]string{
		"unknown":        strings.TrimSuffix(valid, "}") + `,"unknown":true}`,
		"duplicate":      strings.TrimSuffix(valid, "}") + `,"id":"input"}`,
		"trailing":       valid + `{}`,
		"prose":          valid + ` not JSON`,
		"array":          "[" + valid + "]",
		"null":           "null",
		"oversized":      strings.Repeat(" ", memoryInputLimit+1),
		"owner mismatch": strings.Replace(valid, `"owner":"athena"`, `"owner":"dreo"`, 1),
		"invalid ID":     strings.Replace(valid, `"id":"input"`, `"id":"../bad"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			if out, err := runMemoryCLI(t, filepath.Join(parent, "memory"), input, "memory", "record", "--owner", "athena", "--file", "-"); err == nil {
				t.Fatal("invalid input accepted", out)
			}
			assertMemoryRootAbsent(t, parent)
		})
	}
	parent := t.TempDir()
	if out, err := runMemoryCLI(t, filepath.Join(parent, "memory"), valid, "memory", "correct", "other-id", "--owner", "athena", "--revision", "1", "--file", "-"); err == nil {
		t.Fatal("mismatched correction ID accepted", out)
	}
	assertMemoryRootAbsent(t, parent)
}

func TestMemoryJSONSingleRecordImportExportRoundTrip(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	root := filepath.Join(t.TempDir(), "memory")
	input := syntheticMemory("roundtrip", "athena", "episode", "reported")
	first := recordSyntheticMemory(t, root, input)
	second := recordSyntheticMemory(t, root, input)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("idempotent record changed", first, second)
	}
	out, err := runMemoryCLI(t, root, "", "memory", "export", "--owner", "athena", "--limit", "1")
	if err != nil {
		t.Fatal(out, err)
	}
	var page []memory.Record
	if err := json.Unmarshal([]byte(out), &page); err != nil || len(page) != 1 {
		t.Fatal(out, err)
	}
	if !reflect.DeepEqual(page[0], first) {
		t.Fatal("export did not preserve record", page, first)
	}
	// Export is a page of Records, not bulk import. Explicitly project one Input.
	file := filepath.Join(t.TempDir(), "one-input.json")
	if err := os.WriteFile(file, []byte(memoryJSON(t, page[0].Input)), 0600); err != nil {
		t.Fatal(err)
	}
	otherRoot := filepath.Join(t.TempDir(), "memory")
	out, err = runMemoryCLI(t, otherRoot, "", "memory", "record", "--owner", "athena", "--file", file)
	if err != nil {
		t.Fatal(out, err)
	}
	var imported memory.Record
	if err := json.Unmarshal([]byte(out), &imported); err != nil || !reflect.DeepEqual(imported.Input, first.Input) {
		t.Fatal("one-record input roundtrip failed", out, err)
	}
	out, err = runMemoryCLI(t, root, "", "memory", "export", "--owner", "athena", "--after", first.ID)
	if err != nil {
		t.Fatal(out, err)
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil || len(page) != 0 {
		t.Fatal("cursor repeated record", out, err)
	}
	parent := t.TempDir()
	if out, err := runMemoryCLI(t, filepath.Join(parent, "memory"), memoryJSON(t, first), "memory", "record", "--owner", "athena", "--file", "-"); err == nil {
		t.Fatal("server-managed Record fields accepted as Input", out)
	}
	assertMemoryRootAbsent(t, parent)
}

func TestMemoryCorrectForgetAndHumanConvention(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "agent:synthetic")
	parent := t.TempDir()
	root := filepath.Join(parent, "memory")
	in := syntheticMemory("corrected", "athena", "episode", "reported")
	if out, err := runMemoryCLI(t, root, memoryJSON(t, in), "memory", "record", "--owner", "athena", "--file", "-"); err == nil {
		t.Fatal("agent source passed human convention", out)
	}
	assertMemoryRootAbsent(t, parent)
	// Explicit override is intentionally a same-user convention, not authentication.
	out, err := runMemoryCLI(t, root, memoryJSON(t, in), "memory", "record", "--owner", "athena", "--file", "-", "--source", "human:cli")
	if err != nil {
		t.Fatal(out, err)
	}
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	in.Content = "Synthetic corrected violet lantern observation"
	out, err = runMemoryCLI(t, root, memoryJSON(t, in), "memory", "correct", in.ID, "--owner", "athena", "--revision", "1", "--file", "-")
	if err != nil {
		t.Fatal(out, err)
	}
	var corrected memory.Record
	if err := json.Unmarshal([]byte(out), &corrected); err != nil || corrected.Revision != 2 || corrected.Content != in.Content {
		t.Fatal(out, err)
	}
	if out, err := runMemoryCLI(t, root, memoryJSON(t, in), "memory", "correct", in.ID, "--owner", "athena", "--revision", "1", "--file", "-"); err == nil {
		t.Fatal("stale correction succeeded", out)
	}
	out, err = runMemoryCLI(t, root, "", "memory", "show", in.ID, "--owner", "athena", "--history")
	var history []memory.Record
	if err != nil {
		t.Fatal(out, err)
	}
	if err := json.Unmarshal([]byte(out), &history); err != nil || len(history) != 2 {
		t.Fatal("corrected history missing", out, err)
	}
	if out, err := runMemoryCLI(t, root, "", "memory", "forget", in.ID, "--owner", "athena", "--revision", "1", "--confirm"); err == nil {
		t.Fatal("stale forget succeeded", out)
	}
	if out, err := runMemoryCLI(t, root, "", "memory", "forget", in.ID, "--owner", "athena", "--revision", "2", "--confirm"); err != nil {
		t.Fatal(out, err)
	}
	if out, err := runMemoryCLI(t, root, "", "memory", "show", in.ID, "--owner", "athena"); err == nil {
		t.Fatal("forgotten record visible", out)
	}
	if out, err := runMemoryCLI(t, root, memoryJSON(t, in), "memory", "record", "--owner", "athena", "--file", "-"); err == nil {
		t.Fatal("forgotten ID resurrected", out)
	}
}

type memoryFileSnapshot struct {
	Mode fs.FileMode
	Data string
}

func snapshotMemoryFiles(t *testing.T, root string) map[string]memoryFileSnapshot {
	t.Helper()
	files := map[string]memoryFileSnapshot{}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		var data []byte
		if !d.IsDir() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		files[path] = memoryFileSnapshot{Mode: info.Mode(), Data: string(data)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return files
}

func TestDreamViewerIsReadOnlyAndScoped(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	root := filepath.Join(t.TempDir(), "memory")
	dream := recordSyntheticMemory(t, root, syntheticMemory("dream-one", "athena", "dream", "speculative"))
	recordSyntheticMemory(t, root, syntheticMemory("episode-one", "athena", "episode", "reported"))
	recordSyntheticMemory(t, root, syntheticMemory("dreo-one", "dreo", "memory", "reported"))
	before := snapshotMemoryFiles(t, root)
	t.Setenv("DAYLOG_SOURCE", "agent:synthetic")
	for _, args := range [][]string{
		{"dreams"}, {"dreams", "--owners", "athena,dreo"},
		{"dreams", "--status", "speculative", "--project", dream.Project, "--topic", dream.Topic, "--context", dream.Context, "--limit", "1"},
	} {
		out, err := runMemoryCLI(t, root, "", args...)
		if err != nil {
			t.Fatal(out, err)
		}
		var records []memory.Record
		if err := json.Unmarshal([]byte(out), &records); err != nil || len(records) != 1 || records[0].ID != dream.ID || records[0].Owner != "athena" {
			t.Fatal("dream scope leaked", out, err)
		}
	}
	out, err := runMemoryCLI(t, root, "", "dreams", dream.ID)
	if err != nil {
		t.Fatal(out, err)
	}
	var shown memory.Record
	if err := json.Unmarshal([]byte(out), &shown); err != nil || shown.ID != dream.ID {
		t.Fatal(out, err)
	}
	for _, args := range [][]string{
		{"dreams", "episode-one"}, {"dreams", "dreo-one"},
		{"dreams", "--file", "-"}, {"dreams", "--generate"},
		{"dreams", "forget", dream.ID}, {"dreams", "--kind", "episode"},
	} {
		if out, err := runMemoryCLI(t, root, "{}", args...); err == nil {
			t.Fatal("dream viewer accepted non-dream or write request", out)
		}
	}
	if after := snapshotMemoryFiles(t, root); !reflect.DeepEqual(before, after) {
		t.Fatal("dream viewer modified store files or modes")
	}
}

func TestMemoryCrossOwnerSourcesRequireExplicitReadScope(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	root := filepath.Join(t.TempDir(), "memory")
	source := recordSyntheticMemory(t, root, syntheticMemory("source", "dreo", "memory", "reported"))
	derived := syntheticMemory("derived", "athena", "dream", "speculative")
	derived.Sources = []memory.Reference{{Owner: source.Owner, ID: source.ID, Revision: source.Revision, Hash: source.Hash}}
	recordSyntheticMemory(t, root, derived)
	for _, args := range [][]string{
		{"memory", "show", derived.ID, "--owner", "athena"},
		{"dreams", derived.ID},
	} {
		if out, err := runMemoryCLI(t, root, "", args...); err == nil || strings.Contains(out, derived.Content) {
			t.Fatal("cross-owner derived text leaked without scope", out, err)
		}
	}
	for _, args := range [][]string{
		{"memory", "show", derived.ID, "--owner", "athena", "--owners", "athena,dreo"},
		{"dreams", derived.ID, "--owners", "athena,dreo"},
	} {
		if out, err := runMemoryCLI(t, root, "", args...); err != nil || !strings.Contains(out, derived.Content) {
			t.Fatal("explicit source scope failed", out, err)
		}
	}
	out, err := runMemoryCLI(t, root, "", "dreams")
	var records []memory.Record
	if err != nil {
		t.Fatal(out, err)
	}
	if err := json.Unmarshal([]byte(out), &records); err != nil || len(records) != 0 {
		t.Fatal("implicit dream listing leaked derived text", out, err)
	}
}

func TestMemoryRejectsUnconfirmedPreferenceAndInvalidRecall(t *testing.T) {
	t.Setenv("DAYLOG_SOURCE", "human:cli")
	root := filepath.Join(t.TempDir(), "memory")
	pref := syntheticMemory("preference", "dreo", "preference", "confirmed")
	if out, err := runMemoryCLI(t, root, memoryJSON(t, pref), "memory", "record", "--owner", "dreo", "--file", "-"); err == nil {
		t.Fatal("JSON alone confirmed a preference", out)
	}
	if out, err := runMemoryCLI(t, root, memoryJSON(t, pref), "memory", "record", "--owner", "dreo", "--file", "-", "--confirm-preference"); err != nil {
		t.Fatal(out, err)
	}
	for _, query := range []string{strings.Repeat("x", 513), "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen", "***"} {
		if out, err := runMemoryCLI(t, root, "", "memory", "recall", query, "--owner", "dreo"); err == nil {
			t.Fatal("invalid recall accepted", out)
		}
	}
}
