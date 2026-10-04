package collect

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/juanmhidalgo/devclean/internal/config"
)

// Artifact is a directory the walker identified as a build artifact.
type Artifact struct {
	Path string
	Type string
}

// WalkResult is the walker's output. The walker only identifies; it does not
// classify.
type WalkResult struct {
	Artifacts []Artifact
	Roots     []string
	// Unreadable lists paths that could not be read; not an error.
	Unreadable []string
}

// WalkArtifacts finds artifact directories under roots. A directory matches
// when its name equals a type's name AND that type's marker exists (Lstat
// only, never opened). Symlinks are never followed or matched, a match is
// not descended into, and hidden directories are skipped unless the hidden
// name is itself an artifact name (.venv, .tox).
func WalkArtifacts(roots []string, types []config.ArtifactType, excludes []string) WalkResult {
	res := WalkResult{Roots: roots}
	names := map[string]bool{}
	for _, t := range types {
		names[t.Name] = true
	}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				res.Unreadable = append(res.Unreadable, p)
				return nil
			}
			if !d.IsDir() { // WalkDir uses Lstat: symlinks are not dirs
				return nil
			}
			if excluded(p, excludes) {
				return filepath.SkipDir
			}
			name := d.Name()
			if p != root && strings.HasPrefix(name, ".") && !names[name] {
				return filepath.SkipDir
			}
			for _, t := range types {
				if t.Name == name && markerPresent(p, t) {
					res.Artifacts = append(res.Artifacts, Artifact{Path: p, Type: t.Name})
					return filepath.SkipDir
				}
			}
			return nil
		})
	}
	return res
}

func markerPresent(dir string, t config.ArtifactType) bool {
	base := filepath.Dir(dir)
	if t.MarkerLocation == "inside" {
		base = dir
	}
	_, err := os.Lstat(filepath.Join(base, t.Marker))
	return err == nil
}

func excluded(p string, excludes []string) bool {
	for _, e := range excludes {
		if p == e || strings.HasPrefix(p, strings.TrimRight(e, "/")+"/") {
			return true
		}
		if ok, _ := filepath.Match(e, p); ok {
			return true
		}
	}
	return false
}
