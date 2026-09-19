package memory

import (
	"golang.org/x/sys/unix"
	"testing"
)

func TestFilesystemLocalityClassifier(t *testing.T) {
	if !localFilesystem(unix.MNT_LOCAL) || localFilesystem(0) || localFilesystem(unix.MNT_RDONLY) {
		t.Fatal("remote/unknown mount accepted or local mount refused")
	}
}
