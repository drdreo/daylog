//go:build !windows

package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func checkLinks(info os.FileInfo) error {
	if !info.IsDir() {
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
			return fmt.Errorf("hard-linked memory files are unsupported")
		}
	}
	return nil
}
func checkPrivate(path string, info os.FileInfo, dir bool) error {
	if !dir {
		parent, err := os.Stat(filepath.Dir(path))
		if err != nil {
			return err
		}
		fileStat, okFile := info.Sys().(*syscall.Stat_t)
		parentStat, okParent := parent.Sys().(*syscall.Stat_t)
		if !okFile || !okParent || fileStat.Dev != parentStat.Dev {
			return fmt.Errorf("memory files must share the root filesystem")
		}
	}
	want := os.FileMode(0600)
	if dir {
		want = 0700
	}
	if info.Mode().Perm() != want {
		return fmt.Errorf("memory paths must already be private (%04o)", want)
	}
	return nil
}
