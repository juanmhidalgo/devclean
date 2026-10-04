package collect

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/config"
)

func touch(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestWalkArtifacts(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Outside the root: must never be reached through a symlink.
	touch(t, filepath.Join(outside, "package.json"))
	mkdir(t, filepath.Join(outside, "node_modules"))

	touch(t, filepath.Join(root, "a/package.json"))
	mkdir(t, filepath.Join(root, "a/node_modules/dep/node_modules"))
	touch(t, filepath.Join(root, "a/node_modules/dep/package.json"))
	mkdir(t, filepath.Join(root, "b/node_modules"))
	mkdir(t, filepath.Join(root, "c/.venv"))
	touch(t, filepath.Join(root, "c/.venv/pyvenv.cfg"))
	mkdir(t, filepath.Join(root, "d/.venv"))
	touch(t, filepath.Join(root, "e/tox.ini"))
	touch(t, filepath.Join(root, "e/pyproject.toml"))
	mkdir(t, filepath.Join(root, "e/.tox"))
	touch(t, filepath.Join(root, "f/Cargo.toml"))
	mkdir(t, filepath.Join(root, "f/target"))
	mkdir(t, filepath.Join(root, "g/target"))
	touch(t, filepath.Join(root, "h/BUILD.mark"))
	mkdir(t, filepath.Join(root, "h/out"))
	touch(t, filepath.Join(root, ".hidden/package.json"))
	mkdir(t, filepath.Join(root, ".hidden/node_modules"))
	touch(t, filepath.Join(root, "x/package.json"))
	mkdir(t, filepath.Join(root, "x/node_modules"))
	mkdir(t, filepath.Join(root, "locked/sub"))
	touch(t, filepath.Join(root, "s/package.json"))
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	// A symlinked artifact dir with its marker present is still not an artifact.
	if err := os.Symlink(filepath.Join(outside, "node_modules"), filepath.Join(root, "s/node_modules")); err != nil {
		t.Fatal(err)
	}

	locked := filepath.Join(root, "locked")
	unreadableExpected := os.Geteuid() != 0
	if unreadableExpected {
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	}

	types := []config.ArtifactType{
		{Name: "node_modules", Marker: "package.json", MarkerLocation: "beside"},
		{Name: ".venv", Marker: "pyvenv.cfg", MarkerLocation: "inside"},
		{Name: ".tox", Marker: "tox.ini", MarkerLocation: "beside"},
		{Name: ".tox", Marker: "pyproject.toml", MarkerLocation: "beside"},
		{Name: "target", Marker: "Cargo.toml", MarkerLocation: "beside"},
		{Name: "out", Marker: "BUILD.mark", MarkerLocation: "beside"}, // user-defined
	}

	rows := []struct {
		name string
		path string
		typ  string
		want bool
	}{
		{"node_modules with sibling package.json", "a/node_modules", "node_modules", true},
		// MUTATION: missing marker -> name-only match (AC-18)
		{"node_modules without package.json", "b/node_modules", "", false},
		{".venv with pyvenv.cfg inside", "c/.venv", ".venv", true},
		// MUTATION: missing marker -> name-only match (AC-18)
		{".venv without pyvenv.cfg", "d/.venv", "", false},
		{"hidden artifact name is still found, reported once despite two .tox types", "e/.tox", ".tox", true},
		{"target with Cargo.toml", "f/target", "target", true},
		// MUTATION: missing marker -> name-only match (AC-18)
		{"target without Cargo.toml", "g/target", "", false},
		{"user-defined type", "h/out", "out", true},
		// MUTATION: descend into artifacts (AC-19)
		{"nested node_modules inside an artifact", "a/node_modules/dep/node_modules", "", false},
		// MUTATION: follow symlinks (AC-19)
		{"symlink to dir outside root is not followed", "link/node_modules", "", false},
		// MUTATION: follow symlinks (AC-19)
		{"symlinked artifact dir is not an artifact", "s/node_modules", "", false},
		{"hidden non-artifact dir is skipped", ".hidden/node_modules", "", false},
		{"excluded prefix", "x/node_modules", "", false},
	}

	res := WalkArtifacts([]string{root}, types, []string{filepath.Join(root, "x")})

	got := map[string]string{}
	for _, a := range res.Artifacts {
		if _, dup := got[a.Path]; dup {
			t.Errorf("artifact %s reported twice", a.Path)
		}
		got[a.Path] = a.Type
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			typ, found := got[filepath.Join(root, r.path)]
			if found != r.want {
				t.Fatalf("found=%v want=%v (got %v)", found, r.want, got)
			}
			if found && typ != r.typ {
				t.Fatalf("type=%q want %q", typ, r.typ)
			}
		})
	}
	if len(res.Artifacts) != 5 {
		var ps []string
		for p := range got {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		t.Errorf("want exactly 5 artifacts, got %d: %v", len(ps), ps)
	}

	if unreadableExpected {
		found := false
		for _, u := range res.Unreadable {
			if u == locked {
				found = true
			}
		}
		if !found {
			t.Errorf("unreadable %s not in coverage: %v", locked, res.Unreadable)
		}
	}
	if len(res.Roots) != 1 || res.Roots[0] != root {
		t.Errorf("roots = %v, want [%s]", res.Roots, root)
	}
}

// The walker compares excludes to walked paths textually; with both sides in
// the absolute form config.ExpandPath produces, a relative root still honors
// an exclude written with "~".
func TestWalkArtifactsExcludeWithRelativeRoot(t *testing.T) {
	// MUTATION: see docs/mutation-checks.md (excludes vs relative roots).
	home := t.TempDir()
	touch(t, filepath.Join(home, "work/keep/p/package.json"))
	mkdir(t, filepath.Join(home, "work/keep/p/node_modules"))
	touch(t, filepath.Join(home, "work/q/package.json"))
	mkdir(t, filepath.Join(home, "work/q/node_modules"))
	t.Chdir(filepath.Join(home, "work"))

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	roots := config.ExpandPaths([]string{"."}, home, cwd)
	excludes := config.ExpandPaths([]string{"~/work/keep"}, home, home)
	res := WalkArtifacts(roots, config.Default(home).ArtifactTypes, excludes)
	var got []string
	for _, a := range res.Artifacts {
		got = append(got, a.Path)
	}
	if want := []string{filepath.Join(home, "work/q/node_modules")}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("artifacts = %v, want %v", got, want)
	}
}
