//go:build !darwin && !linux && !windows

package memory

import "fmt"

func checkLocalFilesystem(string) error {
	return fmt.Errorf("memory filesystem validation supports Darwin, Linux and Windows only")
}
