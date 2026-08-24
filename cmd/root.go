// Package cmd wires the daylog CLI — the system's single write path and
// its only read contract (§2, §5).
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/drdreo/daylog/internal/view"
)

var rootCmd = &cobra.Command{
	Use:   "daylog",
	Short: "A personal ledger of what every agent (and the human) did today",
	Long: `daylog keeps a short, structured trail of agent and human activity as
append-only events, and renders it as one daily view.

Producer identity comes from $DAYLOG_SOURCE (e.g. agent:claude); it
defaults to human:cli. Data lives in the platform user data dir,
overridable with $DAYLOG_DIR.`,
	SilenceUsage: true,
}

// Execute runs the CLI. Exit codes are meaningful: 0 success, 1 rejected
// input or lookup failure — and a rejected call never touches disk (§5).
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// resolveSource returns the producer identity: --source flag beats
// $DAYLOG_SOURCE beats the human:cli default.
func resolveSource(flag string) (string, error) {
	s := flag
	if s == "" {
		s = os.Getenv("DAYLOG_SOURCE")
	}
	if s == "" {
		s = "human:cli"
	}
	if err := event.ValidateSource(s); err != nil {
		return "", err
	}
	return s, nil
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

// captureCtx shells out to git for repo/branch (~5ms). Any failure yields
// empty fields, never a failed add — logging must work outside repos.
func captureCtx() event.Ctx {
	var ctx event.Ctx
	if cwd, err := os.Getwd(); err == nil {
		ctx.Cwd = tildify(cwd)
	}
	if out, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		ctx.Branch = out
	}
	if out, err := gitOutput("remote", "get-url", "origin"); err == nil {
		ctx.Repo = ownerRepo(out)
	}
	return ctx
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ownerRepo reduces a git remote URL to "owner/repo" for ref shorthand
// expansion (§4.3). Handles https and ssh forms; anything else is kept
// verbatim so the context stays honest.
func ownerRepo(remote string) string {
	s := strings.TrimSuffix(remote, ".git")
	if i := strings.Index(s, "://"); i >= 0 { // https://host/owner/repo
		s = s[i+3:]
		if j := strings.Index(s, "/"); j >= 0 {
			s = s[j+1:]
		}
	} else if i := strings.Index(s, ":"); i >= 0 && strings.Contains(s[:i], "@") { // git@host:owner/repo
		s = s[i+1:]
	}
	if parts := strings.Split(s, "/"); len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return remote
}

func tildify(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

// resolveTarget finds the event a human-typed target means: a unique
// case-insensitive ULID prefix, or a case-insensitive substring of the
// tldr. Open todos are searched first, then all events, newest first.
// Ambiguity is an error listing the candidates — never a guess.
// The returned event carries its EFFECTIVE type (reclassifications
// applied), so callers judge current state, not the original line.
func resolveTarget(target string) (event.Event, error) {
	all, err := store.ReadAll()
	if err != nil {
		return event.Event{}, err
	}
	if len(all) == 0 {
		return event.Event{}, fmt.Errorf("no events in the store yet")
	}

	reclassified, doneBy, _ := view.Resolutions(all)
	var openTodos, rest []event.Event
	for i := len(all) - 1; i >= 0; i-- { // newest first
		e := all[i]
		if e.Type == event.TypeDone || e.Type == event.TypeReclassify || e.Type == event.TypeTriage {
			continue
		}
		if to, ok := reclassified[e.ID]; ok {
			e.Type = to
		}
		if _, isClosed := doneBy[e.ID]; e.Type == event.TypeTodo && !isClosed {
			openTodos = append(openTodos, e)
		} else {
			rest = append(rest, e)
		}
	}

	for _, pool := range [][]event.Event{openTodos, rest} {
		if matches := matchIn(pool, target); len(matches) == 1 {
			return matches[0], nil
		} else if len(matches) > 1 {
			var lines []string
			for _, m := range matches {
				lines = append(lines, fmt.Sprintf("  %s  %s", event.ShortID(m.ID), m.TLDR))
			}
			return event.Event{}, fmt.Errorf("target %q is ambiguous, matches:\n%s\nuse a longer id prefix", target, strings.Join(lines, "\n"))
		}
	}
	return event.Event{}, fmt.Errorf("no event matches %q (tried id prefix and tldr substring)", target)
}

func matchIn(pool []event.Event, target string) []event.Event {
	t := strings.ToUpper(target)
	var byID []event.Event
	for _, e := range pool {
		if strings.HasPrefix(e.ID, t) {
			byID = append(byID, e)
		}
	}
	if len(byID) > 0 {
		return byID
	}
	lt := strings.ToLower(target)
	var byTLDR []event.Event
	for _, e := range pool {
		if strings.Contains(strings.ToLower(e.TLDR), lt) {
			byTLDR = append(byTLDR, e)
		}
	}
	return byTLDR
}

func newEvent(now time.Time, source, typ, tldr string) event.Event {
	return event.Event{
		ID:     event.NewID(now),
		TS:     now.Format(time.RFC3339),
		Host:   hostname(),
		Source: source,
		Type:   typ,
		TLDR:   tldr,
		Refs:   []string{},
		Meta:   map[string]any{},
	}
}
