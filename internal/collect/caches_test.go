package collect

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/platform"
	"github.com/juanmhidalgo/devclean/internal/testenv"
)

func writeSized(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cachesFixture isolates the tool env vars and returns a collector rooted in
// temp dirs plus those dirs.
func cachesFixture(t *testing.T) (c *Caches, home, cacheDir, dataDir string) {
	t.Helper()
	for _, k := range []string{"npm_config_cache", "PIP_CACHE_DIR", "GOMODCACHE", "GOCACHE", "CARGO_HOME", "UV_CACHE_DIR", "YARN_CACHE_FOLDER", "YARN_GLOBAL_FOLDER", "PIPENV_CACHE_DIR", "PNPM_STORE_DIR"} {
		t.Setenv(k, "")
	}
	root := t.TempDir()
	home, cacheDir, dataDir = filepath.Join(root, "home"), filepath.Join(root, "cache"), filepath.Join(root, "data")
	c = &Caches{
		Home: home, CacheDir: cacheDir, DataDir: dataDir,
		Mount: func(string) (platform.Mount, error) { return platform.Mount{Device: "8:1"}, nil },
	}
	return c, home, cacheDir, dataDir
}

func findCand(res Result, path string) (classify.Candidate, bool) {
	for _, c := range res.Candidates {
		if c.Path == path {
			return c, true
		}
	}
	return classify.Candidate{}, false
}

func TestCachesCollector(t *testing.T) {
	t.Run("failing tools are skipped with status and output, locations resolved without npm", func(t *testing.T) {
		c, home, cacheDir, _ := cachesFixture(t)
		testenv.FakeBin(t, "uv", `echo "no version is set for shim"; exit 1`)
		testenv.FakeBin(t, "pre-commit", `exit 2`)
		marker := filepath.Join(t.TempDir(), "npm-ran")
		testenv.FakeBin(t, "npm", `touch `+marker)
		cargo := filepath.Join(t.TempDir(), "cargo")
		t.Setenv("CARGO_HOME", cargo)
		t.Setenv("GOMODCACHE", filepath.Join(t.TempDir(), "gomod"))
		npm := filepath.Join(home, ".npm", "_cacache")
		pip := filepath.Join(cacheDir, "pip")
		gobuild := filepath.Join(cacheDir, "go-build")
		cargoReg := filepath.Join(cargo, "registry", "cache")
		gomod := os.Getenv("GOMODCACHE")
		pipenv := filepath.Join(cacheDir, "pipenv")
		for _, p := range []string{npm, pip, gobuild, cargoReg, gomod, pipenv} {
			writeSized(t, filepath.Join(p, "f"), 10)
		}

		res := c.Collect(context.Background())

		want := map[string]string{
			"uv":         "uv: exit status 1: no version is set for shim",
			"pre-commit": "pre-commit: exit status 2: no output",
		}
		if len(res.Skipped) != 2 {
			t.Fatalf("skipped = %+v", res.Skipped)
		}
		for _, s := range res.Skipped {
			if s.Collector != "caches" {
				t.Errorf("collector = %q", s.Collector)
			}
			found := false
			for _, w := range want {
				found = found || s.Reason == w
			}
			if !found {
				t.Errorf("unexpected skip reason %q", s.Reason)
			}
		}
		for _, p := range []string{npm, pip, gobuild, cargoReg, gomod, pipenv} {
			cand, ok := findCand(res, p)
			if !ok {
				t.Errorf("missing candidate %s", p)
				continue
			}
			if cand.Tier != classify.TierCaches || cand.Category != classify.CategoryCaches || cand.FSID != "8:1" || cand.Size != 10 {
				t.Errorf("candidate %s = %+v", p, cand)
			}
		}
		if _, err := os.Stat(marker); err == nil {
			t.Error("npm was invoked")
		}
	})

	t.Run("yarn berry global cache is manual in both default locations", func(t *testing.T) {
		// Yarn uses $XDG_DATA_HOME/yarn/berry when XDG_DATA_HOME is set and
		// ~/.yarn/berry otherwise, so one machine can hold both.
		c, home, _, dataDir := cachesFixture(t)
		paths := []string{filepath.Join(home, ".yarn", "berry", "cache"), filepath.Join(dataDir, "yarn", "berry", "cache")}
		for _, p := range paths {
			writeSized(t, filepath.Join(p, "f"), 5)
		}
		res := c.Collect(context.Background())
		for _, p := range paths {
			cand, ok := findCand(res, p)
			if !ok || cand.Tier != classify.TierManual {
				t.Errorf("%s: cand = %+v ok=%v", p, cand, ok)
			}
		}
	})

	t.Run("YARN_GLOBAL_FOLDER replaces the default yarn berry locations", func(t *testing.T) {
		c, home, _, dataDir := cachesFixture(t)
		global := filepath.Join(t.TempDir(), "yarn-global")
		t.Setenv("YARN_GLOBAL_FOLDER", global)
		def := filepath.Join(home, ".yarn", "berry", "cache")
		xdg := filepath.Join(dataDir, "yarn", "berry", "cache")
		for _, p := range []string{def, xdg, filepath.Join(global, "cache")} {
			writeSized(t, filepath.Join(p, "f"), 5)
		}
		res := c.Collect(context.Background())
		if cand, ok := findCand(res, filepath.Join(global, "cache")); !ok || cand.Tier != classify.TierManual {
			t.Errorf("global cand = %+v ok=%v", cand, ok)
		}
		for _, p := range []string{def, xdg} {
			if _, ok := findCand(res, p); ok {
				t.Errorf("default location %s reported despite YARN_GLOBAL_FOLDER", p)
			}
		}
	})

	t.Run("user cache_paths keep their tier", func(t *testing.T) {
		c, _, _, _ := cachesFixture(t)
		dir := t.TempDir()
		writeSized(t, filepath.Join(dir, "f"), 7)
		c.Config = config.Config{CachePaths: []config.CachePath{{Path: dir, Tier: "garbage"}}}
		cand, ok := findCand(c.Collect(context.Background()), dir)
		if !ok || cand.Tier != classify.TierGarbage || cand.Size != 7 {
			t.Fatalf("cand = %+v ok=%v", cand, ok)
		}
	})

	t.Run("working tools yield garbage prune actions", func(t *testing.T) {
		c, _, _, _ := cachesFixture(t)
		testenv.FakeBin(t, "uv", `exit 0`)
		testenv.FakeBin(t, "pre-commit", `exit 0`)
		res := c.Collect(context.Background())
		for path, cmd := range map[string]string{"uv cache prune": "uv cache prune", "pre-commit gc": "pre-commit gc"} {
			cand, ok := findCand(res, path)
			if !ok || cand.Tier != classify.TierGarbage || cand.ReclaimCmd != cmd {
				t.Errorf("%s = %+v ok=%v", path, cand, ok)
			}
		}
		if len(res.Skipped) != 0 {
			t.Errorf("skipped = %+v", res.Skipped)
		}
	})
}

func TestCachesRevalidator(t *testing.T) {
	c, home, _, _ := cachesFixture(t)
	dir := filepath.Join(home, "go", "pkg", "mod")
	writeSized(t, filepath.Join(dir, "m", "f"), 10)
	res := c.Collect(context.Background())
	reval := res.Revalidators[dir]
	if reval == nil {
		t.Fatalf("no revalidator for %s", dir)
	}
	if d := reval(context.Background()); d.Tier != classify.TierCaches {
		t.Fatalf("unchanged dir: tier = %v (%s)", d.Tier, d.Reason)
	}
	// Replaced by a symlink since the report: must not qualify.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, dir); err != nil {
		t.Fatal(err)
	}
	if d := reval(context.Background()); d.Tier != classify.TierNone {
		t.Fatalf("symlink: tier = %v", d.Tier)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if d := reval(context.Background()); d.Tier != classify.TierNone {
		t.Fatalf("missing: tier = %v", d.Tier)
	}
}
