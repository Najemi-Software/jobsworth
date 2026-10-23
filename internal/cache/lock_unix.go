//go:build unix

package cache

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// AcquireLock takes the exclusive lock, waiting up to wait for another
// holder to release it. Readers never take it; only removal does.
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
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Lock{f: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
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
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}
