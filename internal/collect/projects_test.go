package collect

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/platform"
)

var projectsNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// makeProject creates a git repo under root with an old commit, old git
// metadata, and a node_modules holding one package whose package.json has the
// given atime. Returns the node_modules path.
func makeProject(t *testing.T, root, name string, atime time.Time) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := filepath.Join(root, name)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-q")
	writeFile(t, filepath.Join(repo, "package.json"), 0o644)
	runGit(t, repo, "add", "package.json")
	runGit(t, repo, "commit", "-q", "-m", "c")
	old := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, f := range []string{"HEAD", "index"} {
		if err := os.Chtimes(filepath.Join(repo, ".git", f), old, old); err != nil {
			t.Fatal(err)
		}
	}
	nm := filepath.Join(repo, "node_modules")
	pkg := filepath.Join(nm, "dep", "package.json")
	writeFile(t, pkg, 0o644)
	if err := os.Chtimes(pkg, atime, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(nm, old, old); err != nil {
		t.Fatal(err)
	}
	return nm
}

func projectsCollector(root string, mount platform.Mount, h history.History) *Projects {
	cfg := config.Default("/nonexistent-home")
	cfg.Scan.Roots = []string{root}
	return &Projects{
		Config:  cfg,
		Mount:   func(string) (platform.Mount, error) { return mount, nil },
		History: h,
		Now:     func() time.Time { return projectsNow },
	}
}

func TestProjectsNoatime(t *testing.T) {
	root := t.TempDir()
	recent := projectsNow.Add(-time.Hour)
	a := makeProject(t, root, "a", recent)
	b := makeProject(t, root, "b", recent)
	c := projectsCollector(root, platform.Mount{Point: root, Device: "8:2", NoAtime: true}, history.History{})

	res := c.Collect(context.Background())

	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], root) {
		t.Fatalf("warnings = %q, want exactly one naming %s", res.Warnings, root)
	}
	got := map[string]classify.Candidate{}
	for _, cand := range res.Candidates {
		got[cand.Path] = cand
	}
	for _, p := range []string{a, b} {
		cand, ok := got[p]
		if !ok {
			t.Fatalf("%s not a candidate despite recent marker atime on noatime mount; got %v", p, got)
		}
		if cand.Tier != classify.TierStale || cand.Category != classify.CategoryProjects || cand.FSID != "8:2" || cand.Size != 2 {
			t.Errorf("candidate = %+v", cand)
		}
	}
	if len(res.Observations) != 0 {
		t.Errorf("noatime reads must not be recorded as observations: %v", res.Observations)
	}
}

func TestProjectsCoverage(t *testing.T) {
	root := t.TempDir()
	a := makeProject(t, root, "a", projectsNow.Add(-time.Hour))
	if err := os.Mkdir(filepath.Join(root, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, "locked"), 0o755) })
	res := projectsCollector(root, platform.Mount{Point: root, Device: "8:2", NoAtime: true}, history.History{}).Collect(context.Background())

	if len(res.Coverage) != 1 {
		t.Fatalf("coverage = %+v", res.Coverage)
	}
	cov := res.Coverage[0]
	if cov.Category != "projects" || !cov.Complete || len(cov.Roots) != 1 || cov.Roots[0] != root {
		t.Errorf("coverage = %+v", cov)
	}
	if !cov.Seen["projects:"+a] {
		t.Errorf("seen = %v, want projects:%s", cov.Seen, a)
	}
	if os.Geteuid() != 0 && (len(cov.Unreadable) != 1 || filepath.Base(cov.Unreadable[0]) != "locked") {
		t.Errorf("unreadable = %v", cov.Unreadable)
	}
}

func TestProjectsHistoryLastUse(t *testing.T) {
	root := t.TempDir()
	a := makeProject(t, root, "a", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	mount := platform.Mount{Point: root, Device: "8:2"}

	res := projectsCollector(root, mount, history.History{}).Collect(context.Background())
	if len(res.Candidates) != 1 {
		t.Fatalf("baseline: want 1 candidate, got %+v", res.Candidates)
	}
	h := history.History{Entries: map[string]history.Entry{"projects:" + a: {LastUsed: projectsNow.Add(-24 * time.Hour)}}}
	res = projectsCollector(root, mount, h).Collect(context.Background())
	if len(res.Candidates) != 0 {
		t.Errorf("recent history last-use should keep artifact fresh, got %+v", res.Candidates)
	}
}

func TestProjectsScanBurst(t *testing.T) {
	root := t.TempDir()
	mount := platform.Mount{Point: root, Device: "8:2"}
	base := projectsNow.Add(-time.Hour)

	t.Run("burst reads are ignored", func(t *testing.T) {
		root := t.TempDir()
		makeProject(t, root, "a", base)
		makeProject(t, root, "b", base.Add(time.Minute))
		c := projectsCollector(root, mount, history.History{})
		c.Config.ScanBurst.MinArtifacts = 2
		res := c.Collect(context.Background())
		if len(res.Candidates) != 2 || len(res.Observations) != 0 {
			t.Errorf("candidates=%d observations=%v, want 2 and none", len(res.Candidates), res.Observations)
		}
	})
	t.Run("isolated read counts as evidence and is observed", func(t *testing.T) {
		root := t.TempDir()
		makeProject(t, root, "a", base)
		makeProject(t, root, "b", base.Add(-48*time.Hour))
		c := projectsCollector(root, mount, history.History{})
		c.Config.ScanBurst.MinArtifacts = 2
		res := c.Collect(context.Background())
		if len(res.Candidates) != 0 || len(res.Observations) != 2 {
			t.Errorf("candidates=%d observations=%v, want 0 and 2", len(res.Candidates), res.Observations)
		}
	})
}

func TestProjectsRevalidator(t *testing.T) {
	root := t.TempDir()
	a := makeProject(t, root, "a", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	mount := platform.Mount{Point: root, Device: "8:2"}
	p := projectsCollector(root, mount, history.History{})
	res := p.Collect(context.Background())
	reval := res.Revalidators[a]
	if reval == nil {
		t.Fatalf("no revalidator for %s", a)
	}
	if d := reval(context.Background()); d.Tier != classify.TierStale {
		t.Fatalf("unchanged artifact: tier = %v (%s), want stale", d.Tier, d.Reason)
	}
	// Used since the report: history now records a recent use.
	p.History = history.History{Entries: map[string]history.Entry{"projects:" + a: {LastUsed: projectsNow.Add(-time.Hour)}}}
	if d := reval(context.Background()); d.Tier != classify.TierNone {
		t.Fatalf("used since report: tier = %v (%s), want none", d.Tier, d.Reason)
	}
}
