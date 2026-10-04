// Package history persists the cleanup history.
package history

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ErrRealHomeUnderTest is returned when a test tries to open a history path
// under the developer's real $HOME.
var ErrRealHomeUnderTest = errors.New("history: refusing to open a path under the real $HOME under go test")

// realHome is captured at package init, before any t.Setenv can change it.
var realHome = os.Getenv("HOME")

// Open validates path for use as a history location. Under go test it refuses
// any path inside the real $HOME.
func Open(path string) error {
	if testing.Testing() && underDir(path, realHome) {
		return ErrRealHomeUnderTest
	}
	return nil
}

func underDir(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
