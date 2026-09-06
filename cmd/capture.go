package cmd

import (
	"fmt"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/capture/adapters"
	"github.com/drdreo/daylog/internal/config"
	original "github.com/drdreo/daylog/internal/context"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/spf13/cobra"
	"io"
	"os"
	"path/filepath"
	"time"
)

func init() {
	var adapter, version string
	c := &cobra.Command{Use: "capture --adapter <pi|claude|codex> --harness-version VERSION", Short: "Enqueue a supported native terminal hook from stdin; return neutral JSON", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if os.Getenv("DAYLOG_INTERNAL") == "1" {
			return printJSON(cmd, map[string]any{})
		}
		b, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 65537))
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		q, err := capture.Open()
		if err != nil {
			return err
		}
		h, err := adapters.ParseHook(adapter, version, b)
		if err == nil && h.Internal {
			return printJSON(cmd, map[string]any{})
		}
		if err == nil && h.Event == "SessionStart" {
			if !adapters.Approved(cfg, adapter, h.Cwd, "") {
				err = fmt.Errorf("project not approved for capture")
			} else {
				ctx := original.At(h.Cwd)
				ctx.Session = h.Session
				ctx.Task = h.Task
				ctx.ParentSession = h.Parent
				err = durable.JSON(filepath.Join(q.Root, "cursors", "context-"+capture.Hash([]byte(adapter+"/"+h.Session))+".json"), adapters.SavedContext{Version: adapters.ParserVersion, Context: ctx})
			}
		} else if err == nil {
			_, err = adapters.CaptureHook(q, cfg, adapter, version, b, time.Now())
		}
		// No raw payload or text enters health telemetry, including on parser failure.
		health := map[string]any{"version": 1, "adapter": adapter, "harness_version": version, "observed_at": time.Now().Format(time.RFC3339Nano), "error": ""}
		if err != nil {
			health["error"] = err.Error()
		}
		if capture.SafeID(adapter) {
			if e := durable.JSON(filepath.Join(q.Root, "cursors", "health-"+adapter+".json"), health); e != nil {
				return e
			}
		}
		if err != nil {
			return err
		}
		return printJSON(cmd, map[string]any{})
	}}
	c.Flags().StringVar(&adapter, "adapter", "", "native harness")
	c.Flags().StringVar(&version, "harness-version", "", "verified installed harness version")
	rootCmd.AddCommand(c)
	rootCmd.AddCommand(&cobra.Command{Use: "reconcile", Short: "Incrementally recover saved evidence from explicitly approved scopes", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, e := config.Load()
		if e != nil {
			return e
		}
		q, e := capture.Open()
		if e != nil {
			return e
		}
		res, e := adapters.Reconcile(q, cfg, time.Now())
		if err := printJSON(cmd, res); err != nil {
			return err
		}
		return e
	}})
}
