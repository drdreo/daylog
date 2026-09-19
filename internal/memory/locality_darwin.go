package memory

import (
	"fmt"
	"golang.org/x/sys/unix"
)

func localFilesystem(flags uint32) bool { return flags&unix.MNT_LOCAL != 0 }
func checkLocalFilesystem(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return err
	}
	if !localFilesystem(stat.Flags) {
		return fmt.Errorf("memory requires a local filesystem (remote mounts are unsupported)")
	}
	return nil
}
