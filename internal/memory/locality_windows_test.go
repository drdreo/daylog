package memory

import (
	"golang.org/x/sys/windows"
	"testing"
)

func TestFilesystemLocalityClassifier(t *testing.T) {
	for _, kind := range []uint32{windows.DRIVE_FIXED, windows.DRIVE_REMOVABLE, windows.DRIVE_RAMDISK} {
		if !localFilesystem(kind) {
			t.Fatal("local drive refused")
		}
	}
	for _, kind := range []uint32{windows.DRIVE_REMOTE, windows.DRIVE_UNKNOWN, windows.DRIVE_NO_ROOT_DIR} {
		if localFilesystem(kind) {
			t.Fatal("remote/unknown drive accepted")
		}
	}
}
