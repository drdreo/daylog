package setup

import (
	"encoding/json"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/durable"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScratchInstallPreservesHooksAndUninstalls(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "fresh data")
	os.MkdirAll(data, 0700)
	cfg := config.Defaults()
	cfg.Runner.AgentDir = filepath.Join(home, ".pi", "agent")
	for _, h := range []string{"pi", "claude", "codex"} {
		cfg.CaptureScopes = append(cfg.CaptureScopes, config.Scope{Harness: h, Directory: filepath.Join(home, "native"), Projects: []string{home}})
	}
	p := filepath.Join(home, ".claude", "settings.json")
	old := map[string]any{"unrelated": true, "hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo unrelated"}}}}}}
	if e := durable.JSON(p, old); e != nil {
		t.Fatal(e)
	}
	i, e := New(home, data, filepath.Join(home, "bin space", "daylog"), "darwin")
	if e != nil {
		t.Fatal(e)
	}
	for n := 0; n < 2; n++ {
		if e := i.Adapters(cfg); e != nil {
			t.Fatal(e)
		}
		if e := i.Skills(); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := i.Schedule(); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), "echo unrelated") != 1 || strings.Count(string(b), "--adapter claude") != 4 {
		t.Fatal(string(b))
	}
	if e := i.Uninstall(); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(p)
	var after map[string]any
	json.Unmarshal(b, &after)
	if after["unrelated"] != true || !strings.Contains(string(b), "echo unrelated") || strings.Contains(string(b), "--adapter") {
		t.Fatal(string(b))
	}
}
func TestRefuseUnownedAndModifiedArtifacts(t *testing.T) {
	h := t.TempDir()
	i, _ := New(h, h, filepath.Join(h, "daylog"), "linux")
	p := filepath.Join(h, "unrelated")
	os.WriteFile(p, []byte("keep"), 0600)
	if i.file(p, []byte("new")) == nil {
		t.Fatal("overwrite")
	}
	p = filepath.Join(h, "owned")
	if e := i.file(p, []byte("owned")); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte("human edit"), 0600)
	if i.Uninstall() == nil {
		t.Fatal("deleted modified artifact")
	}
}
func TestSchedulerPathEscaping(t *testing.T) {
	for _, platform := range []string{"darwin", "linux", "windows"} {
		h := t.TempDir()
		i, _ := New(h, h, filepath.Join(h, "a & b's $dir%", "daylog"), platform)
		p, e := i.Schedule()
		if e != nil {
			t.Fatal(e)
		}
		b, _ := os.ReadFile(p)
		if len(b) == 0 {
			t.Fatal("empty scheduler")
		}
		if platform == "darwin" && !strings.Contains(string(b), "&amp;") {
			t.Fatal("XML unescaped")
		}
	}
}
