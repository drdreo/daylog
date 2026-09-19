package memory

import (
	"fmt"
	"golang.org/x/sys/windows"
)

func localFilesystem(kind uint32) bool {
	return kind == windows.DRIVE_FIXED || kind == windows.DRIVE_REMOVABLE || kind == windows.DRIVE_RAMDISK
}
func checkLocalFilesystem(path string) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	volume := make([]uint16, windows.MAX_PATH+1)
	if err = windows.GetVolumePathName(name, &volume[0], uint32(len(volume))); err != nil {
		return err
	}
	if !localFilesystem(windows.GetDriveType(&volume[0])) {
		return fmt.Errorf("memory requires a local drive (remote or unknown drives are unsupported)")
	}
	return nil
}
