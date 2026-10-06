// Package setup finds what a first config needs and renders it. It only
// reads the filesystem: asking the user and writing the file are the
// caller's.
package setup

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Root is a candidate scan root and how many git repositories it holds.
type Root struct {
	Path  string
	Repos int
}

// FindRoots returns home's non-hidden subdirectories holding git
// repositories at most depth levels below them, most repositories first. It
// never descends into a repository or a hidden directory, and never follows
// symlinks.
func FindRoots(home string, depth int) []Root {
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}
	var roots []Root
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(home, e.Name())
		if n := countRepos(dir, depth); n > 0 {
			roots = append(roots, Root{Path: dir, Repos: n})
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Repos > roots[j].Repos })
	return roots
}

// countRepos counts directories holding a .git entry, dir itself included,
// up to depth levels below dir.
func countRepos(dir string, depth int) int {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return 1
	}
	if depth == 0 {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			n += countRepos(filepath.Join(dir, e.Name()), depth-1)
		}
	}
	return n
}

// diskImageExts are the extensions of VM disks and installer images: large
// files people keep on purpose and forget.
var diskImageExts = map[string]bool{
	".qcow2": true, ".vmdk": true, ".vdi": true, ".vhd": true, ".vhdx": true,
	".img": true, ".iso": true, ".ova": true,
}

// WatchGroup is a glob matching disk images of one extension in one
// directory, with what it matched when found.
type WatchGroup struct {
	Glob  string
	Files int
	Size  int64
}

// FindDiskImages returns one glob per directory and extension holding disk
// images of at least minSize bytes, largest first. A file counts when its
// path below home has at most depth components. Hidden directories are
// skipped and symlinks are not followed.
func FindDiskImages(home string, minSize int64, depth int) []WatchGroup {
	groups := map[string]*WatchGroup{}
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(home, p)
		if d.IsDir() {
			if p != home && (strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator))+1 >= depth) {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !d.Type().IsRegular() || !diskImageExts[ext] {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() < minSize {
			return nil
		}
		glob := filepath.Join(filepath.Dir(p), "*"+filepath.Ext(d.Name()))
		g := groups[glob]
		if g == nil {
			g = &WatchGroup{Glob: glob}
			groups[glob] = g
		}
		g.Files++
		g.Size += info.Size()
		return nil
	})
	out := make([]WatchGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Size != out[j].Size {
			return out[i].Size > out[j].Size
		}
		return out[i].Glob < out[j].Glob
	})
	return out
}

// Tilde writes a path under home as "~/...", the portable form for the
// config file; other paths are returned unchanged.
func Tilde(home, p string) string {
	if p == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}
