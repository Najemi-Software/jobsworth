//go:build windows

package cache

import (
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// AcquireLock takes the exclusive lock with LockFileEx, waiting up to wait
// for another holder to release it (spec-cache §4.2).
func AcquireLock(c *Cache, wait time.Duration) (*Lock, error) {
	dir := filepath.Join(c.Root, SchemaVersion)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, LockFile), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wait)
	for {
		var ol windows.Overlapped
		err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
		if err == nil {
			return &Lock{f: f}, nil
		}
		if err != windows.ERROR_LOCK_VIOLATION && err != windows.ERROR_IO_PENDING {
			_ = f.Close()
			return nil, err
		}
		if LockContended != nil {
			LockContended()
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, ErrLocked
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Unlock releases the lock.
func Unlock(l *Lock) {
	if l == nil || l.f == nil {
		return
	}
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &ol)
	_ = l.f.Close()
	l.f = nil
}
