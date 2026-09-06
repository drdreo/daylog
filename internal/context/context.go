// Package context captures original producer context; workers never recapture it.
package context

import (
	"context"
	"github.com/drdreo/daylog/internal/event"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func Capture() event.Context {
	cwd, _ := os.Getwd()
	c := At(cwd)
	c.Session = first("DAYLOG_SESSION_ID", "PI_SESSION_ID", "CLAUDE_SESSION_ID", "CODEX_THREAD_ID")
	c.Turn = first("DAYLOG_TURN_ID", "CODEX_TURN_ID")
	c.Task = os.Getenv("DAYLOG_TASK_ID")
	c.ParentSession = os.Getenv("DAYLOG_PARENT_SESSION_ID")
	return c
}
func At(cwd string) event.Context {
	c := event.Context{Cwd: cwd}
	if cwd == "" {
		return c
	}
	deadline, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	c.Worktree = git(deadline, cwd, "rev-parse", "--show-toplevel")
	c.Branch = git(deadline, cwd, "branch", "--show-current")
	c.Head = git(deadline, cwd, "rev-parse", "HEAD")
	c.Repository = Repository(git(deadline, cwd, "remote", "get-url", "origin"))
	return c
}
func git(ctx context.Context, cwd string, args ...string) string {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	b, err := cmd.Output()
	if err != nil {
		return ""
	}
	if len(b) > 4096 {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func Repository(remote string) event.Repository {
	if strings.HasPrefix(remote, "git@") {
		remote = "ssh://" + strings.Replace(remote, ":", "/", 1)
	}
	u, err := url.Parse(remote)
	if err != nil || u.Hostname() == "" {
		return event.Repository{}
	}
	path := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
	if len(strings.Split(path, "/")) != 2 {
		return event.Repository{}
	}
	return event.Repository{Host: strings.ToLower(u.Hostname()), Path: path}
}
func first(keys ...string) string {
	for _, k := range keys {
		if s := os.Getenv(k); s != "" {
			return s
		}
	}
	return ""
}

// Within accepts a deleted worktree lexically, but resolves symlinks when present.
func Within(path, root string) bool {
	if !filepath.IsAbs(path) || !filepath.IsAbs(root) {
		return false
	}
	var ok bool
	if path, ok = resolveExistingPrefix(path); !ok {
		return false
	}
	if root, ok = resolveExistingPrefix(root); !ok {
		return false
	}
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Canonicalize existing ancestors even when the final worktree was deleted.
// Dangling symlinks and permission errors are not a lexical approval fallback.
func resolveExistingPrefix(path string) (string, bool) {
	probe := filepath.Clean(path)
	suffix := []string{}
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			return filepath.Join(append([]string{resolved}, suffix...)...), true
		}
		if !os.IsNotExist(err) {
			return "", false
		}
		if _, err := os.Lstat(probe); !os.IsNotExist(err) {
			return "", false
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", false
		}
		suffix = append([]string{filepath.Base(probe)}, suffix...)
		probe = parent
	}
}
