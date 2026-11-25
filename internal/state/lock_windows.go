//go:build windows

package state

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockFileExclusive(f *os.File) error {
	var ov windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ov)
}

func unlockFile(f *os.File) error {
	var ov windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ov)
}
