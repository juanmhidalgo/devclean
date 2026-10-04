package lockfile

import (
	"errors"
	"path/filepath"
	"testing"
)

// AC-29: a second clean run must not start while one holds the clean lock.
func TestTryLockHeld(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T, dir string)
	}{
		{"second holder gets ErrLocked", func(t *testing.T, dir string) {
			path := filepath.Join(dir, "clean.lock")
			unlock, err := TryLock(path)
			if err != nil {
				t.Fatalf("first TryLock: %v", err)
			}
			defer unlock()
			if _, err := TryLock(path); !errors.Is(err, ErrLocked) {
				t.Fatalf("second TryLock err = %v, want ErrLocked", err)
			}
		}},
		{"succeeds after release", func(t *testing.T, dir string) {
			path := filepath.Join(dir, "clean.lock")
			unlock, err := TryLock(path)
			if err != nil {
				t.Fatalf("first TryLock: %v", err)
			}
			if err := unlock(); err != nil {
				t.Fatalf("unlock: %v", err)
			}
			unlock2, err := TryLock(path)
			if err != nil {
				t.Fatalf("TryLock after release: %v", err)
			}
			unlock2()
		}},
		{"history lock does not block clean lock", func(t *testing.T, dir string) {
			hist, err := Lock(filepath.Join(dir, "history.json.lock"))
			if err != nil {
				t.Fatalf("history Lock: %v", err)
			}
			defer hist()
			unlock, err := TryLock(filepath.Join(dir, "clean.lock"))
			if err != nil {
				t.Fatalf("TryLock with history held: %v", err)
			}
			unlock()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(t, t.TempDir()) })
	}
}
