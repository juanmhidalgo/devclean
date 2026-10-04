package collect

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/platform"
)

const cachesCategory = "caches"

// Caches collects package-manager caches and tool-native prune actions.
//
// Cache locations are resolved without invoking the tool: from the tool's env
// var when set, else from a base dir injected by the caller (CacheDir comes
// from platform.CacheDir(); Home is the user's home directory). The collector
// never spells an XDG fallback itself.
//
// Tools needed for a native prune (uv, pre-commit) are probed with
// `<tool> --version`. A tool that is not installed yields no candidate and no
// skip; one that is installed but fails is reported as a Skip with its exit
// status and output ("no output" when empty), never dropped silently.
//
// FSID is Mount(path).Device so the pressure gate can select `caches` items.
// Caches have no history, so the result carries no Coverage and no
// Observations. pnpm's store is only collected when $PNPM_STORE_DIR is set,
// since its default lives under a data directory platform does not expose.
type Caches struct {
	Config   config.Config
	Mount    func(path string) (platform.Mount, error)
	Home     string
	CacheDir string
}

var _ Collector = (*Caches)(nil)

// Name returns the collector's category name.
func (c *Caches) Name() string { return cachesCategory }

// envOr returns $key when set, else fallback.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type cacheDir struct {
	path string
	tier classify.Tier
	why  string
}

// Collect gathers cache candidates and prune actions.
func (c *Caches) Collect(ctx context.Context) Result {
	res := Result{Revalidators: map[string]func(context.Context) classify.Decision{}}

	cargo := envOr("CARGO_HOME", filepath.Join(c.Home, ".cargo"))
	dirs := []cacheDir{
		{filepath.Join(envOr("npm_config_cache", filepath.Join(c.Home, ".npm")), "_cacache"), classify.TierCaches, "npm cache"},
		{envOr("PIP_CACHE_DIR", filepath.Join(c.CacheDir, "pip")), classify.TierCaches, "pip cache"},
		{envOr("GOMODCACHE", filepath.Join(c.Home, "go", "pkg", "mod")), classify.TierCaches, "Go module cache"},
		{envOr("GOCACHE", filepath.Join(c.CacheDir, "go-build")), classify.TierCaches, "Go build cache"},
		{filepath.Join(cargo, "registry", "cache"), classify.TierCaches, "cargo registry cache"},
		{envOr("UV_CACHE_DIR", filepath.Join(c.CacheDir, "uv")), classify.TierCaches, "uv cache"},
		{envOr("YARN_CACHE_FOLDER", filepath.Join(c.CacheDir, "yarn")), classify.TierCaches, "yarn classic cache"},
		{filepath.Join(c.Home, ".yarn", "berry", "cache"), classify.TierManual, "yarn berry global cache; projects may depend on it"},
	}
	if p := os.Getenv("PNPM_STORE_DIR"); p != "" {
		dirs = append(dirs, cacheDir{p, classify.TierCaches, "pnpm store"})
	}
	for _, cp := range c.Config.CachePaths {
		dirs = append(dirs, cacheDir{cp.Path, tierFromName(cp.Tier), "user-defined cache path"})
	}
	for _, d := range dirs {
		if cand, ok := c.dirCandidate(d.path, d.tier, d.why); ok {
			res.Revalidators[cand.Path] = dirRevalidator(cand.Path, cand.Tier)
			res.Candidates = append(res.Candidates, cand)
		}
	}

	for _, t := range []struct{ tool, action string }{
		{"uv", "uv cache prune"},
		{"pre-commit", "pre-commit gc"},
	} {
		out, err := exec.CommandContext(ctx, t.tool, "--version").CombinedOutput()
		var exit *exec.ExitError
		switch {
		case err == nil:
			res.Candidates = append(res.Candidates, classify.Candidate{
				Category: classify.CategoryCaches, Tier: classify.TierGarbage, Path: t.action,
				Reason: "tool-native prune", ReclaimCmd: t.action,
			})
		case errors.As(err, &exit):
			msg := string(bytes.TrimSpace(out))
			if msg == "" {
				msg = "no output"
			}
			res.Skipped = append(res.Skipped, Skip{
				Collector: cachesCategory,
				Reason:    fmt.Sprintf("%s: exit status %d: %s", t.tool, exit.ExitCode(), msg),
			})
		case errors.Is(err, exec.ErrNotFound):
			// Not installed: nothing to prune.
		default:
			res.Skipped = append(res.Skipped, Skip{Collector: cachesCategory, Reason: fmt.Sprintf("%s: %v", t.tool, err)})
		}
	}
	return res
}

func tierFromName(s string) classify.Tier {
	switch strings.ToLower(s) {
	case "garbage":
		return classify.TierGarbage
	case "manual":
		return classify.TierManual
	}
	return classify.TierCaches
}

// dirCandidate builds a candidate for an existing cache dir; a missing dir
// yields none.
func (c *Caches) dirCandidate(path string, tier classify.Tier, why string) (classify.Candidate, bool) {
	if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
		return classify.Candidate{}, false
	}
	cand := classify.Candidate{
		Category: classify.CategoryCaches, Tier: tier, Path: path,
		Size: dirSize(path), Reason: why,
	}
	if tier == classify.TierManual {
		cand.ReclaimCmd = "rm -rf " + path
	}
	if c.Mount != nil {
		if m, err := c.Mount(path); err == nil {
			cand.FSID = m.Device
		}
	}
	return cand, true
}

// dirRevalidator keeps a cache directory's tier only while it still exists as
// a real directory (not a symlink, not gone).
func dirRevalidator(path string, tier classify.Tier) func(context.Context) classify.Decision {
	return func(context.Context) classify.Decision {
		fi, err := os.Lstat(path)
		switch {
		case err != nil:
			return classify.Decision{Tier: classify.TierNone, Reason: "cache dir no longer exists"}
		case fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir():
			return classify.Decision{Tier: classify.TierNone, Reason: "cache path is no longer a plain directory"}
		}
		return classify.Decision{Tier: tier, Reason: "cache dir still present"}
	}
}
