package platform

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var forbidden = []string{
	"/proc", "/var/lib/snapd", "systemd", "journalctl", "snap ",
	"systemctl", ".local/state", ".config", ".cache",
}

// repoRoot walks up from the working directory to the directory with go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func TestNoLinuxSpecificsOutsidePlatform(t *testing.T) {
	root := repoRoot(t)
	platformDir := filepath.Join(root, "internal", "platform")
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "vendor" || n == ".feature-dev" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		if filepath.Dir(path) == platformDir && strings.Contains(name, "linux") {
			return nil
		}
		scanned++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, lit := range forbidden {
			if strings.Contains(string(data), lit) {
				t.Errorf("%s contains Linux-specific literal %q; move it to internal/platform/*linux*.go", rel, lit)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("scanned no files; walker is broken")
	}
}
