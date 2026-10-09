// Package store holds the file handling that the todo and note stores share:
// atomic JSON replacement, append-only JSONL logs, monthly log names and the
// single-writer lock on a data directory. It knows nothing about tasks or
// notes.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrLocked is returned by Lock when another process already holds the
// directory.
var ErrLocked = errors.New("data directory is locked by another process")

// WriteFileAtomic writes data to a temp file beside path, syncs it and renames
// it into place, so a reader never sees half a file. The file is mode 0600.
func WriteFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting permissions on temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// AppendLine appends line plus a newline to path, creating it (0600) when
// missing.
func AppendLine(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("appending to %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", path, err)
	}
	return nil
}

// MonthlyLogPath returns dir/events-YYYY-MM.jsonl for the month of at, in at's
// own location.
func MonthlyLogPath(dir string, at time.Time) string {
	return filepath.Join(dir, "events-"+at.Format("2006-01")+".jsonl")
}

// Lock is an exclusive lock on a data directory.
type Lock struct {
	f *os.File
}

// AcquireLock takes a non-blocking exclusive flock on dir/.lock. A second
// writer would overwrite the state file from an old snapshot, so a held lock
// is an error (ErrLocked) rather than a wait.
func AcquireLock(dir string) (*Lock, error) {
	f, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("locking %s: %w", dir, err)
	}
	return &Lock{f: f}, nil
}

// Close releases the lock. It is safe to call more than once.
func (l *Lock) Close() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close() // closing the descriptor drops the flock
	l.f = nil
	return err
}
