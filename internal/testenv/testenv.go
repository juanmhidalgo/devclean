// Package testenv is test support: it isolates XDG directories so tests never
// touch the developer's real state or config.
package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

// Isolate points XDG_STATE_HOME and XDG_CONFIG_HOME at distinct directories
// under t.TempDir() and returns them. It uses t.Setenv, so callers cannot be
// parallel.
func Isolate(t *testing.T) (state, config string) {
	t.Helper()
	root := t.TempDir()
	state = filepath.Join(root, "state")
	config = filepath.Join(root, "config")
	t.Setenv("XDG_STATE_HOME", state)
	t.Setenv("XDG_CONFIG_HOME", config)
	return state, config
}

// FakeBin writes an executable "#!/bin/sh" script called name into a fresh
// temp directory and prepends that directory to PATH, so the script shadows
// any real binary of the same name. It returns the directory. It uses
// t.Setenv, so callers cannot be parallel.
func FakeBin(t *testing.T, name, script string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}
