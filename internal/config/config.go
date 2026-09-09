// Package config defines typed machine-local settings, never a routing bypass.
package config

import (
	"fmt"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/internal/store"
	"os"
	"path/filepath"
)

const Version = 2

type Scope struct {
	Harness   string   `json:"harness"`
	Directory string   `json:"directory"`
	Projects  []string `json:"projects"`
}
type Runner struct {
	Arguments      []string `json:"arguments"`
	CredentialType string   `json:"credential_type,omitempty"` // Legacy config field; Pi now resolves authentication itself.
	Binary         string   `json:"binary"`
	Path           string   `json:"path"`
	AgentDir       string   `json:"agent_dir"`
	Provider       string   `json:"provider"`
	Model          string   `json:"model"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	MaxInputBytes  int      `json:"max_input_bytes"`
	MaxOutputBytes int      `json:"max_output_bytes"`
	CallsPerRun    int      `json:"calls_per_run"`
	CallsPerDay    int      `json:"calls_per_day"`
	MaxAttempts    int      `json:"max_attempts"`
}
type Config struct {
	Version               int      `json:"version"`
	Mode                  string   `json:"mode"`
	GHOwners              string   `json:"github_owners"`
	Runner                Runner   `json:"runner"`
	QuietSeconds          int      `json:"quiet_seconds"`
	MaxWaitSeconds        int      `json:"max_wait_seconds"`
	BatchSize             int      `json:"batch_size"`
	EvidenceRetentionDays int      `json:"evidence_retention_days"`
	CloudProjects         []string `json:"cloud_projects"`
	CaptureScopes         []Scope  `json:"capture_scopes"`
}

func Defaults() Config {
	return Config{Version: Version, Mode: "live", Runner: Runner{Provider: "openai-codex", Model: "gpt-5.6-luna", TimeoutSeconds: 120, MaxInputBytes: 65536, MaxOutputBytes: 1048576, CallsPerRun: 2, CallsPerDay: 40, MaxAttempts: 3}, QuietSeconds: 60, MaxWaitSeconds: 300, BatchSize: 8, EvidenceRetentionDays: 14, CloudProjects: []string{}, CaptureScopes: []Scope{}}
}
func Path() (string, error) { r, e := store.DataDir(); return filepath.Join(r, "config.json"), e }
func Load() (Config, error) {
	if err := store.Ensure(); err != nil {
		return Config{}, err
	}
	p, e := Path()
	if e != nil {
		return Config{}, e
	}
	c := Defaults()
	c.Version = 0
	if err := durable.Read(p, &c); err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return Config{}, err
	}
	return c, c.Validate()
}
func Save(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := store.Ensure(); err != nil {
		return err
	}
	p, _ := Path()
	return durable.JSON(p, c)
}
func (c Config) Validate() error {
	if c.Version != Version {
		return fmt.Errorf("unsupported config version %d", c.Version)
	}
	// Accept old shadow configs without silently enabling their publication.
	// New installs are live; preview is now a per-invocation CLI flag.
	if c.Mode != "shadow" && c.Mode != "live" {
		return fmt.Errorf("mode must be shadow or live")
	}
	r := c.Runner
	if len(r.Arguments) > 16 {
		return fmt.Errorf("runner.arguments exceeds limit")
	}
	for _, arg := range r.Arguments {
		if len(arg) > 4096 {
			return fmt.Errorf("runner argument exceeds limit")
		}
	}
	if r.Provider == "" || r.Model == "" || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 600 || r.MaxInputBytes < 1024 || r.MaxInputBytes > 262144 || r.MaxOutputBytes < 1024 || r.MaxOutputBytes > 4194304 || r.CallsPerRun < 1 || r.CallsPerRun > 20 || r.CallsPerDay < 1 || r.CallsPerDay > 1000 || r.MaxAttempts < 1 || r.MaxAttempts > 5 {
		return fmt.Errorf("invalid runner limits")
	}
	for _, p := range []string{r.Binary, r.AgentDir} {
		if p != "" && !filepath.IsAbs(p) {
			return fmt.Errorf("runner paths must be absolute")
		}
	}
	if c.QuietSeconds < 0 || c.MaxWaitSeconds < c.QuietSeconds || c.MaxWaitSeconds > 3600 || c.BatchSize < 1 || c.BatchSize > 16 || c.EvidenceRetentionDays < 1 {
		return fmt.Errorf("invalid batching/retention limits")
	}
	for _, p := range c.CloudProjects {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("cloud project must be absolute")
		}
	}
	for _, s := range c.CaptureScopes {
		if s.Harness != "pi" && s.Harness != "claude" && s.Harness != "codex" {
			return fmt.Errorf("unsupported harness %q", s.Harness)
		}
		if !filepath.IsAbs(s.Directory) || len(s.Projects) == 0 {
			return fmt.Errorf("capture scope needs an absolute directory and approved projects")
		}
		for _, p := range s.Projects {
			if !filepath.IsAbs(p) {
				return fmt.Errorf("approved project must be absolute")
			}
		}
	}
	return nil
}
