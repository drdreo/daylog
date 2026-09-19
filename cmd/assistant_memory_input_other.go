//go:build !darwin && !linux

package cmd

import (
	"fmt"
	"os"
)

func prepareMemoryPipe(input *os.File) (*os.File, func() error, error) {
	return nil, nil, fmt.Errorf("stdin does not support bounded reads on this platform; use --file with a regular JSON file instead")
}
