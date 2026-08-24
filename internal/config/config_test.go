package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write puts a config.json in a temp data dir and points DAYLOG_DIR at it.
func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DAYLOG_DIR", dir)
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadMissingFileIsZeroConfig(t *testing.T) {
	write(t, "")
	c, err := Load()
	if err != nil {
		t.Fatalf("a missing config must not be an error, got %v", err)
	}
	if c.GHOwners != "" {
		t.Errorf("config = %+v, want zero", c)
	}
}

func TestLoadOwnerSpecForms(t *testing.T) {
	cases := []struct {
		body, want string
	}{
		{`{"gh_owners": "lovablelabs"}`, "lovablelabs"},
		{`{"gh_owners": "lovablelabs, !oldorg"}`, "lovablelabs, !oldorg"},
		{`{"gh_owners": ["lovablelabs", "!oldorg"]}`, "lovablelabs,!oldorg"},
		{`{"gh_owners": []}`, ""},
		{`{}`, ""},
	}
	for _, c := range cases {
		write(t, c.body)
		got, err := Load()
		if err != nil {
			t.Errorf("%s: %v", c.body, err)
			continue
		}
		if string(got.GHOwners) != c.want {
			t.Errorf("%s: gh_owners = %q, want %q", c.body, got.GHOwners, c.want)
		}
	}
}

func TestLoadRejectsMalformedAndTypos(t *testing.T) {
	cases := []struct{ name, body string }{
		{"truncated json", `{"gh_owners": "x"`},
		{"typo'd key", `{"gh_owner": "lovablelabs"}`},
		{"wrong type", `{"gh_owners": 42}`},
	}
	for _, c := range cases {
		write(t, c.body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: accepted %s — a setting that silently does nothing is worse than an error", c.name, c.body)
		}
	}
}

func TestLoadErrorNamesTheFile(t *testing.T) {
	dir := write(t, `{"gh_owners":`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "config.json")) {
		t.Fatalf("error = %v, want it to name the offending file", err)
	}
}
