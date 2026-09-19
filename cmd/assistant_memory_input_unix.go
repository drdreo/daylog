//go:build darwin || linux

package cmd

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// prepareMemoryPipe gives Go's poller a nonblocking duplicate of an inherited
// pipe. Dup shares file status flags: restore the original flags before closing
// only the duplicate. The caller must stop deadline callbacks before cleanup.
func prepareMemoryPipe(input *os.File) (*os.File, func() error, error) {
	conn, err := input.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var flags int
	duplicate := -1
	var opErr error
	err = conn.Control(func(fd uintptr) {
		flags, opErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
		if opErr != nil {
			return
		}
		duplicate, opErr = unix.FcntlInt(fd, unix.F_DUPFD_CLOEXEC, 0)
		if opErr == nil {
			opErr = unix.SetNonblock(duplicate, true)
		}
	})
	if err != nil || opErr != nil {
		if duplicate >= 0 {
			_ = unix.Close(duplicate)
		}
		if err == nil {
			err = opErr
		}
		return nil, nil, fmt.Errorf("prepare bounded stdin; use a regular --file instead: %w", err)
	}
	pipe := os.NewFile(uintptr(duplicate), "memory input pipe")
	return pipe, func() error {
		// The original descriptor remains owned by the caller. Restore through
		// the duplicate, even if the caller has meanwhile closed the original.
		raw, restoreErr := pipe.SyscallConn()
		if restoreErr == nil {
			var flagsErr error
			restoreErr = raw.Control(func(fd uintptr) {
				_, flagsErr = unix.FcntlInt(fd, unix.F_SETFL, flags)
			})
			if restoreErr == nil {
				restoreErr = flagsErr
			}
		}
		closeErr := pipe.Close()
		if restoreErr != nil {
			return fmt.Errorf("restore stdin file status flags: %w", restoreErr)
		}
		return closeErr
	}, nil
}
