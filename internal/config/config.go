// Package config reads the optional per-machine settings file at
// <data>/config.json. Settings are machine-scoped by design: one laptop is
// one context, and the work machine's PR scope has no business following the
// event log to the home machine. Like the poller snapshots under state/,
// this file is never synced (§4.4, §8) — the event store holds the shared
// narrative, config holds what is true about *this* machine.
//
// Environment variables and flags still win over the file (§5 precedence),
// but the file is what survives a GUI launch: a widget button or a launchd
// job inherits neither the shell profile nor its exports.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/drdreo/daylog/internal/store"
)

// Config is the whole settings file. New keys are added here; every key is
// optional, and its zero value must mean "behave as if unconfigured".
type Config struct {
	GHOwners OwnerSpec `json:"gh_owners"`
}

// OwnerSpec is a GitHub owner filter spec, accepting either form a human
// would reach for:
//
//	"gh_owners": "lovablelabs, !oldorg"
//	"gh_owners": ["lovablelabs", "!oldorg"]
//
// Both reduce to the comma-separated spec the poller parses, so the file,
// the flag, and the env var all speak one syntax.
type OwnerSpec string

func (o *OwnerSpec) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*o = OwnerSpec(s)
		return nil
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return errors.New("gh_owners must be a string or a list of strings")
	}
	*o = OwnerSpec(strings.Join(list, ","))
	return nil
}

// Path returns the settings file location, <data>/config.json.
func Path() (string, error) {
	root, err := store.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "config.json"), nil
}

// Load reads the settings. A missing file is the zero Config, not an error:
// the file is optional and most machines will never have one. Malformed
// JSON or an unknown key IS an error — a typo'd key that quietly did
// nothing would look exactly like a setting that works.
func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()

	var c Config
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}
