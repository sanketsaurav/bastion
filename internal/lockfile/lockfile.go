// Package lockfile provides the local per-box operation lock (SPEC.md §10).
// Reads run concurrently; conflicting mutations fail fast with owner detail.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock is a held advisory file lock.
type Lock struct {
	file *os.File
	path string
}

// Acquire takes an exclusive non-blocking lock for the named box under dir,
// recording pid for diagnostics. A held lock returns an error naming the file
// so a stale lock can be recovered by deleting exactly that file.
func Acquire(dir, name string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		owner, _ := os.ReadFile(path)
		f.Close()
		return nil, fmt.Errorf("another bastion operation holds the lock for this box (%s, owner pid %s); wait for it or remove the file if it is stale", path, string(owner))
	}
	_ = f.Truncate(0)
	fmt.Fprintf(f, "%d", os.Getpid())
	_ = f.Sync()
	return &Lock{file: f, path: path}, nil
}

// Release drops the lock. The file is left in place; flock state is what
// matters, and deleting it here would race with a concurrent Acquire.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
