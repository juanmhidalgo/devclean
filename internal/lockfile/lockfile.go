// Package lockfile provides advisory inter-process locks backed by flock(2).
package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// Lock blocks until it holds an exclusive lock on path, creating the file and
// its parent directory if needed. The returned func releases the lock; it is
// safe to call once.
func Lock(path string) (unlock func() error, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return func() error {
		// Closing the descriptor releases the flock.
		return f.Close()
	}, nil
}

// ErrLocked reports that another holder has the lock.
var ErrLocked = errors.New("lockfile: already locked")

// TryLock is the non-blocking variant of Lock: it returns ErrLocked when
// another descriptor already holds the lock.
func TryLock(path string) (unlock func() error, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, ErrLocked
		}
		return nil, err
	}
	return f.Close, nil
}
