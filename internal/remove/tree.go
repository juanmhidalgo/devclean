// Package remove deletes files and trees.
package remove

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Swapped in tests to simulate failures.
var (
	removeAll = os.RemoveAll
	chmod     = os.Chmod
)

// RemoveTree deletes root and everything below it. A symlink root is removed
// as a link; its target is never touched. If deletion fails with EACCES, the
// owner write bit is ORed into each directory inside root (never outside it,
// never following symlinks) and the removal is retried once.
func RemoveTree(root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(root)
	}
	err = removeAll(root)
	if err == nil || !errors.Is(err, syscall.EACCES) {
		return err
	}
	makeWritable(root, root)
	if retryErr := removeAll(root); retryErr != nil {
		return fmt.Errorf("remove %s: %w (retry after chmod: %v)", root, err, retryErr)
	}
	return nil
}

// makeWritable ORs the owner write bit into every directory under dir that is
// contained in root. Symlinks are never followed.
func makeWritable(root, dir string) {
	if !within(root, dir) {
		return
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	if info.Mode().Perm()&0o200 == 0 {
		_ = chmod(dir, info.Mode()|0o200)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		makeWritable(root, filepath.Join(dir, e.Name()))
	}
}

// within reports whether path is root or below it, component-wise.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
