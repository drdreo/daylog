package cmd

import (
	"errors"
	"fmt"
	"github.com/drdreo/daylog/internal/athena"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/capture/adapters"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/setup"
	"github.com/drdreo/daylog/internal/store"
	"github.com/spf13/cobra"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func init() {
	var piPath, agentDir, mode string
	var projects, scopes, piArgs []string
	var installAdapters, installSkills, schedule, activate, uninstall bool
	c := &cobra.Command{Use: "setup", Short: "Configure fresh machine settings and explicitly opt into capture/resources", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := humanSource(""); err != nil {
			return err
		}
		if mode != "" && mode != "live" {
			return fmt.Errorf("setup only accepts --mode live to resume legacy configurations; use curate --once --dry-run to preview, or stop the scheduler to pause")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		root, err := store.DataDir()
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		installer, err := setup.New(home, root, bin, setup.Platform())
		if err != nil {
			return err
		}
		if uninstall {
			fmt.Fprintln(cmd.OutOrStdout(), "Stop/disable the scheduled job before removing resources. Store and config are retained.")
			return installer.Uninstall()
		}
		if piPath != "" {
			cfg.Runner.Binary, err = filepath.Abs(piPath)
			if err != nil {
				return err
			}
		} else if cfg.Runner.Binary == "" {
			if p, e := exec.LookPath("pi"); e == nil {
				cfg.Runner.Binary, _ = filepath.Abs(p)
			}
		}
		if cmd.Flags().Changed("pi-arg") {
			cfg.Runner.Arguments = piArgs
		}
		if cfg.Runner.Path == "" {
			cfg.Runner.Path = os.Getenv("PATH")
		}
		if agentDir != "" {
			cfg.Runner.AgentDir, err = filepath.Abs(agentDir)
			if err != nil {
				return err
			}
		} else if cfg.Runner.AgentDir == "" {
			cfg.Runner.AgentDir = os.Getenv("PI_CODING_AGENT_DIR")
			if cfg.Runner.AgentDir == "" {
				cfg.Runner.AgentDir = filepath.Join(home, ".pi", "agent")
			}
		}
		approved := []string{}
		for _, p := range projects {
			abs, err := filepath.Abs(p)
			if err != nil {
				return err
			}
			if strings.ContainsAny(abs, "\r\n\x00") {
				return fmt.Errorf("invalid project path")
			}
			approved = append(approved, abs)
			if !contains(cfg.CloudProjects, abs) {
				cfg.CloudProjects = append(cfg.CloudProjects, abs)
			}
		}
		for _, raw := range scopes {
			h, dir, ok := strings.Cut(raw, "=")
			if !ok || adapters.Versions[h] == "" || len(approved) == 0 {
				return fmt.Errorf("--capture-scope harness=/absolute/native/directory requires --approve-project")
			}
			dir, err = filepath.Abs(dir)
			if err != nil {
				return err
			}
			s := config.Scope{Harness: h, Directory: dir, Projects: approved}
			found := false
			for i, old := range cfg.CaptureScopes {
				if old.Harness == h && old.Directory == dir {
					cfg.CaptureScopes[i] = s
					found = true
				}
			}
			if !found {
				cfg.CaptureScopes = append(cfg.CaptureScopes, s)
			}
		}
		if mode != "" {
			cfg.Mode = mode
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
		if installAdapters {
			if err := installer.Adapters(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Adapters installed. Review hook trust in Claude/Codex; restart/reload pi. Trust is never bypassed.")
		}
		if installSkills {
			if err := installer.Skills(); err != nil {
				return err
			}
		}
		if activate && !schedule {
			return fmt.Errorf("--activate requires --schedule")
		}
		if schedule {
			path, err := installer.Schedule()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "scheduler resource:", path)
			if activate {
				var p *exec.Cmd
				switch setup.Platform() {
				case "darwin":
					uid, err := exec.Command("/usr/bin/id", "-u").Output()
					if err != nil {
						return err
					}
					p = exec.Command("/bin/launchctl", "bootstrap", "gui/"+strings.TrimSpace(string(uid)), path)
				case "linux":
					if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
						return err
					}
					p = exec.Command("systemctl", "--user", "enable", "--now", "daylog-athena.timer")
				case "windows":
					p = exec.Command("schtasks.exe", "/Create", "/TN", "DaylogAthena", "/XML", path)
				}
				p.Stdout = cmd.OutOrStdout()
				p.Stderr = cmd.ErrOrStderr()
				if err := p.Run(); err != nil {
					return err
				}
			}
		}
		fmt.Fprintln(cmd.OutOrStdout(), "configured", root, "mode", cfg.Mode)
		return nil
	}}
	c.Flags().StringVar(&piPath, "pi", "", "absolute pi executable or Node launcher path")
	c.Flags().StringArrayVar(&piArgs, "pi-arg", nil, "fixed launcher argument (e.g. absolute pi CLI script for Node); repeatable")
	c.Flags().StringVar(&agentDir, "pi-agent-dir", "", "installed pi auth/catalog directory")
	c.Flags().StringArrayVar(&projects, "approve-project", nil, "approve this project for cloud editing; repeatable")
	c.Flags().StringArrayVar(&scopes, "capture-scope", nil, "approve harness=/native/session/directory for local scanning; repeatable")
	c.Flags().StringVar(&mode, "mode", "", "live: explicitly resume an old shadow configuration (new installs are already live)")
	c.Flags().BoolVar(&installAdapters, "install-adapters", false, "install configured harness adapters without bypassing trust")
	c.Flags().BoolVar(&installSkills, "install-skills", false, "install the same reporting instructions for all harnesses")
	c.Flags().BoolVar(&schedule, "schedule", false, "write platform scheduler resources")
	c.Flags().BoolVar(&activate, "activate", false, "explicitly start/register the scheduled worker")
	c.Flags().BoolVar(&uninstall, "uninstall-resources", false, "remove owned integrations/resources, retain all data")
	rootCmd.AddCommand(c)
	rootCmd.AddCommand(&cobra.Command{Use: "tick", Short: "Scheduled bounded reconciliation followed by curation", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		q, err := capture.Open()
		if err != nil {
			return err
		}
		scan, se := adapters.Reconcile(q, cfg, time.Now())
		w := athena.Worker{Queue: q, Config: cfg, Runner: athena.PiRunner{Config: cfg.Runner}}
		res, we := w.Once(cmd.Context(), false)
		if err := printJSON(cmd, map[string]any{"reconcile": scan, "curate": res}); err != nil {
			return err
		}
		return errors.Join(se, we)
	}})
}
