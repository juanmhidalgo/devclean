package collect

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
)

// GatherArtifactFacts collects the use signals for one artifact using stat
// calls and git metadata only. Marker files are never opened (Lstat only), so
// gathering cannot refresh the atimes it reads. Missing evidence is left zero;
// outside a git work tree, in a directory the enclosing repo tracks nothing
// in, or without git, InGitWorkTree is false, which the classifier treats as
// "never stale".
func GatherArtifactFacts(a Artifact) classify.ArtifactFacts {
	var f classify.ArtifactFacts
	parent := filepath.Dir(a.Path)

	if fi, err := os.Lstat(a.Path); err == nil {
		f.ArtifactMTime = fi.ModTime()
	}

	if out, err := exec.Command("git", "-C", parent, "rev-parse", "--absolute-git-dir").Output(); err == nil {
		gitDir := strings.TrimSpace(string(out))
		// An enclosing repo only speaks for directories it tracks: a home
		// directory versioned for its dotfiles must not lend its activity
		// to ~/Downloads/x/node_modules.
		if gitDir != "" && hasTrackedFiles(parent) {
			f.InGitWorkTree = true
			if fi, err := os.Stat(filepath.Join(gitDir, "HEAD")); err == nil {
				f.HeadMTime = fi.ModTime()
			}
			if fi, err := os.Stat(filepath.Join(gitDir, "index")); err == nil {
				f.IndexMTime = fi.ModTime()
			}
			if out, err := exec.Command("git", "-C", parent, "log", "-1", "--format=%ct").Output(); err == nil {
				if sec, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64); err == nil {
					f.CommitTime = time.Unix(sec, 0)
				}
			}
		}
	}

	for _, m := range markerPaths(a.Path) {
		if fi, err := os.Lstat(m); err == nil {
			if at, ok := atimeOf(fi); ok {
				f.MarkerATimes = append(f.MarkerATimes, at)
			}
		}
	}
	return f
}

// hasTrackedFiles reports whether git tracks at least one file under dir. It
// stops reading after the first path, so a large repo costs no more than a
// small one.
func hasTrackedFiles(dir string) bool {
	cmd := exec.Command("git", "-C", dir, "ls-files", "-z", "--", ".")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	var b [1]byte
	n, _ := io.ReadFull(out, b[:])
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return n == 1
}

// markerPaths lists the marker files whose atime counts as a use signal:
// pyvenv.cfg for a venv, and one level of package.json files for node_modules.
func markerPaths(artifact string) []string {
	if filepath.Base(artifact) == "node_modules" {
		var out []string
		entries, _ := os.ReadDir(artifact)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "@") {
				scoped, _ := os.ReadDir(filepath.Join(artifact, e.Name()))
				for _, s := range scoped {
					out = append(out, filepath.Join(artifact, e.Name(), s.Name(), "package.json"))
				}
				continue
			}
			out = append(out, filepath.Join(artifact, e.Name(), "package.json"))
		}
		return out
	}
	return []string{filepath.Join(artifact, "pyvenv.cfg")}
}
