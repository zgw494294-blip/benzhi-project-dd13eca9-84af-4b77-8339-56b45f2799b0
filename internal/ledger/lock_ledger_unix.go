//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package ledger

import (
	"fmt"
	"os"
	"syscall"
)

// lockLedger acquires an exclusive advisory lock on a sibling lock file for
// the ledger at path. The returned unlock function releases the lock and
// closes the file handle. The lock is process-wide via flock(2), so it works
// across separate CLI processes that operate on the same ledger file. The lock
// file is created next to the ledger; its parent directory is expected to
// already exist (the caller's ledger path already resolves there).
func lockLedger(path string) (func() error, error) {
	lockPath := path + ".lock"
	file, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("open lock file %q: %w", lockPath, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("flock %q: %w", lockPath, err)
	}
	return func() error {
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_UN); err != nil {
			_ = file.Close()
			return fmt.Errorf("unlock %q: %w", lockPath, err)
		}
		return file.Close()
	}, nil
}
