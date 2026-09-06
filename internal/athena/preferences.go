package athena

import (
	"fmt"
	"github.com/drdreo/daylog/internal/durable"
	"os"
)

type Preferences struct {
	Version  int          `json:"version"`
	Examples []Preference `json:"examples"`
}

func LoadPreferences(path string) (Preferences, error) {
	p := Preferences{Version: Version, Examples: []Preference{}}
	if err := durable.Read(path, &p); err != nil && !os.IsNotExist(err) {
		return p, err
	}
	if p.Version != Version {
		return p, fmt.Errorf("unsupported preferences")
	}
	return p, nil
}
