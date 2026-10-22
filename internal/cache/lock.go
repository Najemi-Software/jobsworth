package cache

import (
	"errors"
	"os"
)

// LockFile is the advisory lock under the schema directory (spec-cache
// §3.3, §4.2).
const LockFile = ".lock"

// ErrLocked is returned when another process holds the lock past the wait.
var ErrLocked = errors.New("cache is locked by another process")

// Lock is a held exclusive lock; release with Unlock.
type Lock struct {
	f *os.File
}
