// Package setup installs only explicitly requested, reversible machine resources.
package setup

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/drdreo/daylog/integrations"
	"github.com/drdreo/daylog/internal/capture"
	"github.com/drdreo/daylog/internal/capture/adapters"
	"github.com/drdreo/daylog/internal/config"
	"github.com/drdreo/daylog/internal/durable"
	"github.com/drdreo/daylog/skills"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Artifact struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}
type HookRegistration struct {
	Path    string `json:"path"`
	Event   string `json:"event"`
	Command string `json:"command"`
}
type Manifest struct {
	Version   int                `json:"version"`
	Artifacts []Artifact         `json:"artifacts"`
	Hooks     []HookRegistration `json:"hooks"`
}
type Installer struct {
	Home, Data, Binary, Platform string
	Manifest                     Manifest
}

func New(home, data, binary, platform string) (*Installer, error) {
	i := &Installer{Home: home, Data: data, Binary: binary, Platform: platform, Manifest: Manifest{Version: 2, Artifacts: []Artifact{}, Hooks: []HookRegistration{}}}
	if err := durable.Read(filepath.Join(data, "installation.json"), &i.Manifest); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if i.Manifest.Version != 2 {
		return nil, fmt.Errorf("unsupported installation manifest")
	}
	return i, nil
}
func (i *Installer) save() error {
	return durable.JSON(filepath.Join(i.Data, "installation.json"), i.Manifest)
}
func (i *Installer) file(path string, b []byte) error {
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	owned := false
	for _, a := range i.Manifest.Artifacts {
		if a.Path == path && a.Hash == capture.Hash(old) {
			owned = true
		}
	}
	if err == nil && !owned && string(old) != string(b) {
		return fmt.Errorf("refusing to overwrite unowned/modified file %s", path)
	}
	if err := durable.Write(path, b); err != nil {
		return err
	}
	a := Artifact{path, capture.Hash(b)}
	found := false
	for n := range i.Manifest.Artifacts {
		if i.Manifest.Artifacts[n].Path == path {
			i.Manifest.Artifacts[n] = a
			found = true
		}
	}
	if !found {
		i.Manifest.Artifacts = append(i.Manifest.Artifacts, a)
	}
	return i.save()
}
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (i *Installer) command(h string) string {
	if i.Platform == "windows" {
		return `"` + i.Binary + `" --data-dir "` + i.Data + `" capture --adapter ` + h + ` --harness-version ` + adapters.Versions[h]
	}
	return Quote(i.Binary) + " --data-dir " + Quote(i.Data) + " capture --adapter " + h + " --harness-version " + adapters.Versions[h]
}
func (i *Installer) Adapters(cfg config.Config) error {
	configured := map[string]bool{}
	for _, s := range cfg.CaptureScopes {
		configured[s.Harness] = true
	}
	if len(configured) == 0 {
		return fmt.Errorf("approve capture scopes before installing adapters")
	}
	for _, h := range []string{"pi", "claude", "codex"} {
		if !configured[h] {
			continue
		}
		if h == "pi" {
			dir := cfg.Runner.AgentDir
			if dir == "" {
				dir = filepath.Join(i.Home, ".pi", "agent")
			}
			settings, _ := json.Marshal(map[string]string{"binary": i.Binary, "data_dir": i.Data})
			target := filepath.Join(dir, "extensions", "daylog-athena")
			if err := i.file(filepath.Join(target, "daylog-settings.json"), settings); err != nil {
				return err
			}
			b, _ := integrations.Files.ReadFile("pi/daylog.ts")
			if err := i.file(filepath.Join(target, "index.ts"), b); err != nil {
				return err
			}
			continue
		}
		p := filepath.Join(i.Home, "."+h, "hooks.json")
		events := []string{"SessionStart", "Stop", "SubagentStop"}
		if h == "claude" {
			p = filepath.Join(i.Home, ".claude", "settings.json")
			events = append(events, "StopFailure")
		} else {
			events = append(events, "Interrupt")
		}
		for _, event := range events {
			reg := HookRegistration{p, event, i.command(h)}
			if err := changeHook(reg, false); err != nil {
				return err
			}
			found := false
			for _, r := range i.Manifest.Hooks {
				if r == reg {
					found = true
				}
			}
			if !found {
				i.Manifest.Hooks = append(i.Manifest.Hooks, reg)
			}
			if err := i.save(); err != nil {
				return err
			}
		}
	}
	return nil
}
func changeHook(reg HookRegistration, remove bool) error {
	u, err := durable.Lock(reg.Path+".daylog.lock", true)
	if err != nil {
		return err
	}
	defer u()
	data := map[string]any{}
	b, err := os.ReadFile(reg.Path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &data); err != nil {
			return err
		}
	}
	hooks, ok := data["hooks"].(map[string]any)
	if !ok {
		if data["hooks"] != nil {
			return fmt.Errorf("unexpected hooks format")
		}
		hooks = map[string]any{}
		data["hooks"] = hooks
	}
	groups, ok := hooks[reg.Event].([]any)
	if !ok && hooks[reg.Event] != nil {
		return fmt.Errorf("unexpected hook event format")
	}
	out := []any{}
	found := false
	for _, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			return fmt.Errorf("unexpected matcher group")
		}
		handlers, ok := group["hooks"].([]any)
		if !ok {
			return fmt.Errorf("unexpected handlers")
		}
		kept := []any{}
		for _, raw := range handlers {
			h, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("unexpected handler")
			}
			if h["command"] == reg.Command {
				found = true
				if remove {
					continue
				}
			}
			kept = append(kept, raw)
		}
		if len(kept) > 0 {
			group["hooks"] = kept
			out = append(out, group)
		}
	}
	if !remove && !found {
		timeout := 5
		if reg.Event == "Interrupt" {
			timeout = 3
		}
		out = append(out, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": reg.Command, "timeout": timeout, "statusMessage": "daylog private capture"}}})
	}
	if len(out) == 0 {
		delete(hooks, reg.Event)
	} else {
		hooks[reg.Event] = out
	}
	return durable.JSON(reg.Path, data)
}
func (i *Installer) Skills() error {
	b, _ := skills.Files.ReadFile("daylog/SKILL.md")
	for _, base := range []string{filepath.Join(i.Home, ".agents", "skills"), filepath.Join(i.Home, ".claude", "skills")} {
		if err := i.file(filepath.Join(base, "daylog", "SKILL.md"), b); err != nil {
			return err
		}
	}
	return nil
}
func xmlText(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
func (i *Installer) Schedule() (string, error) {
	switch i.Platform {
	case "darwin":
		p := filepath.Join(i.Home, "Library", "LaunchAgents", "dev.daylog.athena.plist")
		args := []string{i.Binary, "--data-dir", i.Data, "tick"}
		var values strings.Builder
		for _, arg := range args {
			values.WriteString("<string>" + xmlText(arg) + "</string>")
		}
		b := `<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>dev.daylog.athena</string><key>ProgramArguments</key><array>` + values.String() + `</array><key>StartInterval</key><integer>60</integer><key>RunAtLoad</key><true/><key>Umask</key><integer>63</integer><key>WorkingDirectory</key><string>` + xmlText(i.Data) + `</string><key>StandardOutPath</key><string>` + xmlText(filepath.Join(i.Data, "worker.stdout.log")) + `</string><key>StandardErrorPath</key><string>` + xmlText(filepath.Join(i.Data, "worker.stderr.log")) + `</string></dict></plist>`
		return p, i.file(p, []byte(b))
	case "linux":
		dir := filepath.Join(i.Home, ".config", "systemd", "user")
		escape := func(s string) string {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(s) + `"`
		}
		service := "[Unit]\nDescription=Daylog Athena capture and gatekeeping\n[Service]\nType=oneshot\nUMask=0077\nExecStart=" + escape(i.Binary) + " --data-dir " + escape(i.Data) + " tick\n"
		if err := i.file(filepath.Join(dir, "daylog-athena.service"), []byte(service)); err != nil {
			return "", err
		}
		p := filepath.Join(dir, "daylog-athena.timer")
		return p, i.file(p, []byte("[Unit]\nDescription=Daylog Athena one-shot wakeup\n[Timer]\nOnBootSec=1min\nOnUnitActiveSec=1min\n[Install]\nWantedBy=timers.target\n"))
	case "windows":
		p := filepath.Join(i.Data, "daylog-task.xml")
		b := `<?xml version="1.0" encoding="UTF-16"?><Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Triggers><TimeTrigger><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition><StartBoundary>2026-01-01T00:00:00</StartBoundary><Enabled>true</Enabled></TimeTrigger></Triggers><Principals><Principal id="Author"><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><ExecutionTimeLimit>PT10M</ExecutionTimeLimit></Settings><Actions Context="Author"><Exec><Command>` + xmlText(i.Binary) + `</Command><Arguments>--data-dir &quot;` + xmlText(i.Data) + `&quot; tick</Arguments></Exec></Actions></Task>`
		b = strings.Replace(b, "UTF-16", "UTF-8", 1)
		return p, i.file(p, []byte(b))
	default:
		return "", fmt.Errorf("unsupported scheduler platform")
	}
}
func (i *Installer) Uninstall() error {
	for _, r := range i.Manifest.Hooks {
		if err := changeHook(r, true); err != nil {
			return err
		}
	}
	for _, a := range i.Manifest.Artifacts {
		b, err := os.ReadFile(a.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if capture.Hash(b) != a.Hash {
			return fmt.Errorf("modified artifact retained: %s", a.Path)
		}
		if err := os.Remove(a.Path); err != nil {
			return err
		}
	}
	i.Manifest = Manifest{Version: 2, Artifacts: []Artifact{}, Hooks: []HookRegistration{}}
	return i.save()
}
func Platform() string { return runtime.GOOS }
