//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package ledger

// lockLedger is a no-op fallback for platforms without flock(2). Concurrent
// cross-process saves are not serialized on these platforms; callers should
// rely on the platform's own advisory locking if available.
func lockLedger(path string) (func() error, error) {
	return func() error { return nil }, nil
}
