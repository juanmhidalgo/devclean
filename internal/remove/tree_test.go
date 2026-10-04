package remove

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// restoreModes makes every directory under dir writable again so t.TempDir
// cleanup can delete it.
func restoreModes(dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if d != nil && d.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
}

func skipIfRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
}

// buildTree creates parent(0750)/root with read-only subdirs; returns both.
func buildTree(t *testing.T) (parent, root string) {
	t.Helper()
	base := t.TempDir()
	t.Cleanup(func() { restoreModes(base) })
	parent = filepath.Join(base, "parent")
	root = filepath.Join(parent, "root")
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(deep, "f"), []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{deep, filepath.Join(root, "a")} {
		if err := os.Chmod(d, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(parent, 0o750); err != nil {
		t.Fatal(err)
	}
	return parent, root
}

// AC-27
func TestRemoveTreeReadOnly(t *testing.T) {
	skipIfRoot(t)

	t.Run("deletes read-only tree and keeps parent mode", func(t *testing.T) {
		parent, root := buildTree(t)
		if err := RemoveTree(root); err != nil {
			t.Fatalf("RemoveTree: %v", err)
		}
		if _, err := os.Lstat(root); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("root still exists: %v", err)
		}
		st, err := os.Stat(parent)
		if err != nil {
			t.Fatal(err)
		}
		// MUTATION: chmod-ing the parent of root must fail this assertion.
		if got := st.Mode().Perm(); got != 0o750 {
			t.Fatalf("parent mode = %o, want 750", got)
		}
	})

	t.Run("retry failure wraps original error and ORs the write bit", func(t *testing.T) {
		_, root := buildTree(t)
		dir := filepath.Join(root, "a")
		if err := os.Chmod(dir, 0o510); err != nil {
			t.Fatal(err)
		}
		orig := &fs.PathError{Op: "unlinkat", Path: dir, Err: syscall.EACCES}
		old := removeAll
		removeAll = func(string) error { return orig }
		t.Cleanup(func() { removeAll = old })

		err := RemoveTree(root)
		if !errors.Is(err, orig) {
			t.Fatalf("err = %v, want wrapping original", err)
		}
		if !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("err = %v, want fs.ErrPermission", err)
		}
		st, serr := os.Stat(dir)
		if serr != nil {
			t.Fatal(serr)
		}
		// MUTATION: replacing the mode (0700) instead of OR-ing the owner
		// write bit (0710) must fail this assertion.
		if got := st.Mode().Perm(); got != 0o710 {
			t.Fatalf("dir mode = %o, want 710", got)
		}
	})

	t.Run("symlink root removes link only", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(base, "link")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		if err := RemoveTree(link); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("link still exists: %v", err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("target gone: %v", err)
		}
	})

	t.Run("symlink inside tree is not followed", func(t *testing.T) {
		base := t.TempDir()
		outside := filepath.Join(base, "outside")
		if err := os.MkdirAll(outside, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { restoreModes(base) })
		root := filepath.Join(base, "root")
		ro := filepath.Join(root, "ro")
		if err := os.MkdirAll(ro, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(ro, "ln")); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		if err := RemoveTree(root); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(outside)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != 0o555 {
			t.Fatalf("outside mode = %o, want 555", got)
		}
	})
}
