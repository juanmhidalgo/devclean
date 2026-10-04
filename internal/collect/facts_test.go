package collect

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_DATE=2024-01-02T03:04:05Z", "GIT_COMMITTER_DATE=2024-01-02T03:04:05Z")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestGatherArtifactFacts(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	commit := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	headM := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	indexM := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	artM := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	at1 := time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC)
	at2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	setup := func(t *testing.T, mode os.FileMode) (repo string) {
		repo = t.TempDir()
		runGit(t, repo, "init", "-q")
		writeFile(t, filepath.Join(repo, "a.txt"), 0o644)
		runGit(t, repo, "add", "a.txt")
		runGit(t, repo, "commit", "-q", "-m", "c")
		for p, ts := range map[string]time.Time{
			filepath.Join(repo, ".git", "HEAD"):  headM,
			filepath.Join(repo, ".git", "index"): indexM,
		} {
			if err := os.Chtimes(p, ts, ts); err != nil {
				t.Fatal(err)
			}
		}
		return repo
	}
	touch := func(t *testing.T, p string, at, m time.Time) {
		t.Helper()
		if err := os.Chtimes(p, at, m); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("node_modules in git, markers mode 000", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can open mode 000 files")
		}
		repo := setup(t, 0)
		nm := filepath.Join(repo, "node_modules")
		p1 := filepath.Join(nm, "left", "package.json")
		p2 := filepath.Join(nm, "@scope", "right", "package.json")
		writeFile(t, p1, 0)
		writeFile(t, p2, 0)
		touch(t, p1, at1, at1)
		touch(t, p2, at2, at2)
		touch(t, nm, at2, artM)

		f := GatherArtifactFacts(Artifact{Path: nm, Type: "node_modules"})
		if !f.InGitWorkTree {
			t.Fatal("expected InGitWorkTree")
		}
		if !f.CommitTime.Equal(commit) {
			t.Errorf("CommitTime = %v, want %v", f.CommitTime, commit)
		}
		if !f.HeadMTime.Equal(headM) {
			t.Errorf("HeadMTime = %v, want %v", f.HeadMTime, headM)
		}
		if !f.IndexMTime.Equal(indexM) {
			t.Errorf("IndexMTime = %v, want %v", f.IndexMTime, indexM)
		}
		if !f.ArtifactMTime.Equal(artM) {
			t.Errorf("ArtifactMTime = %v, want %v", f.ArtifactMTime, artM)
		}
		got := map[int64]bool{}
		for _, a := range f.MarkerATimes {
			got[a.Unix()] = true
		}
		if len(f.MarkerATimes) != 2 || !got[at1.Unix()] || !got[at2.Unix()] {
			t.Errorf("MarkerATimes = %v, want %v and %v", f.MarkerATimes, at1, at2)
		}
	})

	t.Run("venv pyvenv.cfg atime", func(t *testing.T) {
		repo := setup(t, 0)
		venv := filepath.Join(repo, ".venv")
		cfg := filepath.Join(venv, "pyvenv.cfg")
		writeFile(t, cfg, 0)
		touch(t, cfg, at1, at1)
		f := GatherArtifactFacts(Artifact{Path: venv, Type: "venv"})
		if len(f.MarkerATimes) != 1 || !f.MarkerATimes[0].Equal(at1) {
			t.Errorf("MarkerATimes = %v, want [%v]", f.MarkerATimes, at1)
		}
	})

	t.Run("enclosing repo that tracks nothing in the directory lends no signal", func(t *testing.T) {
		// MUTATION: see docs/mutation-checks.md (ancestor repo signals).
		repo := setup(t, 0) // tracks only a.txt at its root, like a dotfiles home
		untracked := filepath.Join(repo, "Downloads", "x")
		writeFile(t, filepath.Join(untracked, "package.json"), 0o644)
		tracked := filepath.Join(repo, "proj")
		writeFile(t, filepath.Join(tracked, "package.json"), 0o644)
		runGit(t, repo, "add", "proj/package.json")
		runGit(t, repo, "commit", "-q", "-m", "proj")
		for _, d := range []string{untracked, tracked} {
			if err := os.MkdirAll(filepath.Join(d, "node_modules"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if f := GatherArtifactFacts(Artifact{Path: filepath.Join(untracked, "node_modules"), Type: "node_modules"}); f.InGitWorkTree {
			t.Errorf("untracked dir inside an enclosing repo: InGitWorkTree = true, want false")
		}
		if f := GatherArtifactFacts(Artifact{Path: filepath.Join(tracked, "node_modules"), Type: "node_modules"}); !f.InGitWorkTree {
			t.Errorf("tracked subproject: InGitWorkTree = false, want true")
		}
	})

	t.Run("outside git work tree", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "node_modules")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f := GatherArtifactFacts(Artifact{Path: dir, Type: "node_modules"})
		if f.InGitWorkTree {
			t.Error("InGitWorkTree must be false outside a repo")
		}
	})
}
