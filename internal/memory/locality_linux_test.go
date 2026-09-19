package memory

import (
	"golang.org/x/sys/unix"
	"testing"
)

func TestFilesystemLocalityClassifier(t *testing.T) {
	for _, kind := range []int64{unix.EXT4_SUPER_MAGIC, unix.TMPFS_MAGIC, unix.OVERLAYFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.F2FS_SUPER_MAGIC, unix.RAMFS_MAGIC} {
		if !localFilesystem(kind) || !localFilesystem(int64(int32(uint32(kind)))) {
			t.Fatal("supported local filesystem refused, including signed 32-bit representation")
		}
	}
	for _, kind := range []int64{unix.NFS_SUPER_MAGIC, unix.CIFS_SUPER_MAGIC, unix.FUSE_SUPER_MAGIC, 0} {
		if localFilesystem(kind) || localFilesystem(int64(int32(uint32(kind)))) {
			t.Fatal("remote/unknown filesystem accepted")
		}
	}
}
