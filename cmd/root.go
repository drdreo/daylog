package cmd

import (
	"fmt"
	original "github.com/drdreo/daylog/internal/context"
	"github.com/drdreo/daylog/internal/event"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"os"
	"strings"
	"time"
)

var dataDirFlag string
var rootCmd = &cobra.Command{Use: "daylog", Short: "Private agent reports, curated outcomes, and human notes", SilenceUsage: true, PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
	if dataDirFlag != "" {
		return os.Setenv("DAYLOG_DIR", dataDirFlag)
	}
	return nil
}}

func init() {
	rootCmd.PersistentFlags().StringVar(&dataDirFlag, "data-dir", "", "explicit machine store path (overrides DAYLOG_DIR)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
func resolveSource(flag string) (string, error) {
	s := flag
	if s == "" {
		s = os.Getenv("DAYLOG_SOURCE")
	}
	if s == "" {
		s = "human:cli"
	}
	return s, event.ValidateSource(s)
}
func humanSource(flag string) (string, error) {
	s, e := resolveSource(flag)
	if e != nil {
		return "", e
	}
	if !event.Human(s) {
		return "", fmt.Errorf("human-only command; use --source human:cli if you are the human")
	}
	return s, nil
}
func hostname() string          { h, _ := os.Hostname(); return h }
func captureCtx() event.Context { return original.Capture() }
func resolveTarget(target string) (event.Entry, error) {
	all, err := store.ReadAll()
	if err != nil {
		return event.Entry{}, err
	}
	current := event.Effective(all)
	matches := []event.Entry{}
	for _, e := range current {
		if strings.HasPrefix(strings.ToLower(e.ID), strings.ToLower(target)) {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 {
		for _, e := range current {
			if strings.Contains(strings.ToLower(e.TLDR), strings.ToLower(target)) {
				matches = append(matches, e)
			}
		}
	}
	if len(matches) != 1 {
		return event.Entry{}, fmt.Errorf("target %q matches %d entries; use a unique ID", target, len(matches))
	}
	return matches[0], nil
}
func newEvent(now time.Time, source, typ, text string) event.Event {
	id := event.NewID(now)
	return event.Event{Version: event.Version, ID: id, PublicationKey: "human/" + id, RecordedAt: now.Format(time.RFC3339Nano), OccurredAt: now.Format(time.RFC3339Nano), TimeBasis: "human", Host: hostname(), Source: source, Type: typ, TLDR: text, Refs: []string{}}
}
