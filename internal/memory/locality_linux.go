package memory

import (
	"fmt"
	"golang.org/x/sys/unix"
)

// Fail closed on unknown/FUSE filesystems: FUSE may conceal remote storage.
func localFilesystem(kind int64) bool {
	switch kind {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC,
		unix.TMPFS_MAGIC, unix.RAMFS_MAGIC, unix.OVERLAYFS_SUPER_MAGIC,
		unix.F2FS_SUPER_MAGIC:
		return true
	}
	return false
}
func checkLocalFilesystem(path string) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return err
	}
	if !localFilesystem(int64(stat.Type)) {
		return fmt.Errorf("memory requires a supported local filesystem (ext, XFS, Btrfs, tmpfs, ramfs, overlay or F2FS)")
	}
	return nil
}
