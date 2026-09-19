package memory

import (
	"fmt"
	"github.com/drdreo/daylog/internal/durable"
	"os"
	"path/filepath"
)

func prepareRoot(root string, writable bool, local func(string) error) error {
	if err := local(root); err != nil {
		return err
	}
	if writable {
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			if err = durable.Mkdir(root); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return local(root)
}

// Check the nearest existing ancestor before creating anything. The root is
// checked again after creation; every database is a direct, unlinked child.
func checkLocalRoot(root string) error {
	for {
		info, err := os.Stat(root)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("memory root ancestor is not a directory")
			}
			return checkLocalFilesystem(root)
		}
		if !os.IsNotExist(err) {
			return err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return err
		}
		root = parent
	}
}
