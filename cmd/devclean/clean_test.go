package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/lockfile"
	"github.com/juanmhidalgo/devclean/internal/platform"
	"github.com/juanmhidalgo/devclean/internal/remove"
	"github.com/juanmhidalgo/devclean/internal/testenv"
)

// cleanResult is the fixture collector output: one garbage, one cache, three
// stale and one manual candidate on filesystem fs1, all revalidating to their
// own tier. fs1 is at 95% so caches qualify (pressure defaults to 85).
func cleanEnv(t *testing.T) *reportEnv {
	t.Helper()
	e := newReportEnv(t, fakePlatform{euid: 1000, statfs: func(string) platform.FSUsage {
		return platform.FSUsage{ID: "fs1", Used: 95, Avail: 5}
	}})
	mk := func(cat classify.Category, tier classify.Tier, p string) classify.Candidate {
		return classify.Candidate{Category: cat, Tier: tier, Path: p, Size: 1, FSID: "fs1"}
	}
	cands := []classify.Candidate{
		mk(classify.CategoryCaches, classify.TierGarbage, "/t/garbage"),
		mk(classify.CategoryCaches, classify.TierCaches, "/t/caches"),
		mk(classify.CategoryProjects, classify.TierStale, "/t/stale1"),
		mk(classify.CategoryProjects, classify.TierStale, "/t/stale2"),
		mk(classify.CategoryProjects, classify.TierStale, "/t/stale3"),
		mk(classify.CategoryCaches, classify.TierManual, "/t/manual"),
	}
	rv := map[string]func(context.Context) classify.Decision{}
	for _, c := range cands {
		tier := c.Tier
		rv[c.Path] = func(context.Context) classify.Decision { return classify.Decision{Tier: tier} }
	}
	e.a.newCollectors = func(config.Config, history.History) []collect.Collector {
		return []collect.Collector{spyCollector{name: "projects", log: &e.calls, res: collect.Result{Candidates: cands, Revalidators: rv}}}
	}
	return e
}

func (e *reportEnv) tty(answers string) {
	e.a.isTTY = func() bool { return true }
	e.a.stdin = strings.NewReader(answers)
}

// wouldDelete extracts the paths listed under the dry-run "Would delete" header.
func wouldDelete(out string) []string {
	_, rest, ok := strings.Cut(out, "Would delete")
	if !ok {
		return nil
	}
	var got []string
	for _, l := range strings.Split(rest, "\n")[1:] {
		if !strings.HasPrefix(l, "  ") {
			break
		}
		got = append(got, strings.TrimSpace(l))
	}
	return got
}

func TestCleanCommand(t *testing.T) {
	t.Run("dry-run lists exactly what clean deletes, with zero deleter calls (AC-3)", func(t *testing.T) {
		e := cleanEnv(t)
		e.tty("1,3\n")
		if code := e.run("clean", "--dry-run"); code != 0 {
			t.Fatalf("dry-run exit = %d, stderr %q", code, e.stderr.String())
		}
		if len(e.deletes) != 0 {
			t.Fatalf("dry-run called the deleter: %v", e.deletes)
		}
		planned := wouldDelete(e.stdout.String())
		if len(planned) == 0 {
			t.Fatalf("no plan in output:\n%s", e.stdout.String())
		}
		e.tty("1,3\n")
		if code := e.run("clean"); code != 0 {
			t.Fatalf("clean exit = %d, stderr %q", code, e.stderr.String())
		}
		if !reflect.DeepEqual(planned, e.deletes) {
			t.Errorf("dry-run planned %v, clean deleted %v", planned, e.deletes)
		}
	})

	t.Run("stale answer 1,3 deletes exactly items 1 and 3 (AC-2)", func(t *testing.T) {
		e := cleanEnv(t)
		e.tty("1,3\n")
		if code := e.run("clean"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		want := []string{"/t/garbage", "/t/caches", "/t/stale1", "/t/stale3"}
		if !reflect.DeepEqual(e.deletes, want) {
			t.Errorf("deleted %v, want %v", e.deletes, want)
		}
	})

	t.Run("invalid answer re-prompts and EOF means none", func(t *testing.T) {
		e := cleanEnv(t)
		e.tty("9\n")
		if code := e.run("clean"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		want := []string{"/t/garbage", "/t/caches"}
		if !reflect.DeepEqual(e.deletes, want) {
			t.Errorf("deleted %v, want %v", e.deletes, want)
		}
		if !strings.Contains(e.stderr.String(), "out of range") {
			t.Errorf("stderr lacks the parse error: %q", e.stderr.String())
		}
	})

	t.Run("non-TTY without --yes prints the stale-skipped notice (AC-2)", func(t *testing.T) {
		e := cleanEnv(t)
		if code := e.run("clean"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if !strings.Contains(e.stdout.String(), "stale items skipped: not a TTY; pass --yes to delete them") {
			t.Errorf("notice missing:\n%s", e.stdout.String())
		}
		if want := []string{"/t/garbage", "/t/caches"}; !reflect.DeepEqual(e.deletes, want) {
			t.Errorf("deleted %v, want %v", e.deletes, want)
		}
	})

	t.Run("--json is non-interactive even on a TTY", func(t *testing.T) {
		e := cleanEnv(t)
		e.tty("all\n")
		if code := e.run("clean", "--json"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if want := []string{"/t/garbage", "/t/caches"}; !reflect.DeepEqual(e.deletes, want) {
			t.Errorf("deleted %v, want %v", e.deletes, want)
		}
		var doc struct {
			Freed map[string]int64 `json:"freed_bytes_by_filesystem"`
		}
		if err := json.Unmarshal(e.stdout.Bytes(), &doc); err != nil || doc.Freed == nil {
			t.Errorf("not a clean document (err %v):\n%s", err, e.stdout.String())
		}
	})

	t.Run("--tier garbage deletes no stale even with --yes", func(t *testing.T) {
		e := cleanEnv(t)
		if code := e.run("clean", "--yes", "--tier", "garbage"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if want := []string{"/t/garbage"}; !reflect.DeepEqual(e.deletes, want) {
			t.Errorf("deleted %v, want %v", e.deletes, want)
		}
	})

	t.Run("--yes deletes every stale item and reports freed space", func(t *testing.T) {
		e := cleanEnv(t)
		calls := 0
		e.a.platform = withStatfs(e.a.platform.(fakePlatform), func(string) platform.FSUsage {
			calls++
			return platform.FSUsage{ID: "fs1", Used: 95, Avail: uint64(5 + 100*(calls/2))}
		})
		if code := e.run("clean", "--yes"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if len(e.deletes) != 5 {
			t.Errorf("deleted %v, want 5 items", e.deletes)
		}
		if !strings.Contains(e.stdout.String(), "fs1") {
			t.Errorf("freed space not reported:\n%s", e.stdout.String())
		}
	})

	t.Run("a second concurrent clean exits 1 (AC-6)", func(t *testing.T) {
		e := cleanEnv(t)
		unlock, err := lockfile.TryLock(filepath.Join(e.a.platform.StateDir(), "clean.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		if code := e.run("clean", "--yes"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(e.stderr.String(), "another clean is running") {
			t.Errorf("stderr = %q", e.stderr.String())
		}
		if len(e.deletes) != 0 {
			t.Errorf("deleted under a held lock: %v", e.deletes)
		}
	})

	t.Run("history is saved with observations and pruned by coverage (AC-29)", func(t *testing.T) {
		e := cleanEnv(t)
		hp := filepath.Join(e.a.platform.StateDir(), "history.json")
		os.MkdirAll(filepath.Dir(hp), 0o755)
		seed := `{"version":1,"entries":{"projects:/p/gone":{"first_seen":"2025-01-01T00:00:00Z","last_used":"2025-01-01T00:00:00Z"}},"walked":{}}`
		os.WriteFile(hp, []byte(seed), 0o600)
		used := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		inner := e.a.newCollectors
		e.a.newCollectors = func(c config.Config, h history.History) []collect.Collector {
			cs := inner(c, h)
			cs = append(cs, spyCollector{name: "docker", log: &e.calls, res: collect.Result{
				Coverage: []history.Coverage{{Category: "projects", Roots: []string{"/p"}, Complete: true, Seen: map[string]bool{"projects:/p/a": true}}},
				Observations: []collect.Observation{
					{Key: "projects:/p/a", At: used},
					{Key: "docker:sha256:x", At: used, FirstSeenOnly: true},
				}}})
			return cs
		}
		if code := e.run("clean", "--yes"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		h, _, err := history.Load(hp, e.a.now)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := h.Entries["projects:/p/gone"]; ok {
			t.Errorf("unseen entry not pruned: %v", h.Entries)
		}
		if got := h.Entries["projects:/p/a"]; !got.LastUsed.Equal(used) || !got.FirstSeen.Equal(used) {
			t.Errorf("observed entry = %+v", got)
		}
		if got, ok := h.Entries["docker:sha256:x"]; !ok || !got.FirstSeen.Equal(used) || !got.LastUsed.IsZero() {
			t.Errorf("first-seen-only entry = %+v (present %v)", got, ok)
		}
	})

	t.Run("dry-run takes no lock and writes no history", func(t *testing.T) {
		e := cleanEnv(t)
		unlock, err := lockfile.TryLock(filepath.Join(e.a.platform.StateDir(), "clean.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		if code := e.run("clean", "--dry-run", "--yes"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		if _, err := os.Stat(filepath.Join(e.a.platform.StateDir(), "history.json")); err == nil {
			t.Error("dry-run wrote history.json")
		}
	})

	t.Run("notification failure turns the exit code into 2 (AC-39)", func(t *testing.T) {
		e := cleanEnv(t)
		testenv.FakeBin(t, "failnotify", "echo boom; exit 1")
		os.MkdirAll(e.cfgDir, 0o755)
		os.WriteFile(filepath.Join(e.cfgDir, "config.toml"), []byte("notify_command = \"failnotify\"\n"), 0o644)
		if code := e.run("clean", "--yes"); code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
		if !strings.Contains(e.stderr.String(), "boom") {
			t.Errorf("stderr lacks notify error: %q", e.stderr.String())
		}
	})

	t.Run("notification body is the summary, sent on stdin; --quiet suppresses a clean run", func(t *testing.T) {
		e := cleanEnv(t)
		out := filepath.Join(t.TempDir(), "body")
		testenv.FakeBin(t, "savenotify", "cat > "+out)
		os.MkdirAll(e.cfgDir, 0o755)
		os.WriteFile(filepath.Join(e.cfgDir, "config.toml"), []byte("notify_command = \"savenotify\"\n"), 0o644)
		e.a.platform = withStatfs(e.a.platform.(fakePlatform), func(string) platform.FSUsage {
			return platform.FSUsage{ID: "fs1", Used: 10, Avail: 90}
		})
		if code := e.run("clean", "--yes", "--quiet"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		if _, err := os.Stat(out); err == nil {
			t.Error("--quiet still notified on a healthy run")
		}
		if code := e.run("clean", "--yes"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		b, err := os.ReadFile(out)
		if err != nil || !strings.Contains(string(b), "Deleted 4") {
			t.Errorf("body = %q (err %v)", b, err)
		}
		os.Remove(out)
		e.a.platform = withStatfs(e.a.platform.(fakePlatform), func(string) platform.FSUsage {
			return platform.FSUsage{ID: "fs1", Used: 95, Avail: 5}
		})
		e.run("clean", "--yes", "--quiet")
		if _, err := os.Stat(out); err != nil {
			t.Error("--quiet did not notify above pressure")
		}
	})

	t.Run("history save failure after deleting still reports and notifies, exit 1 (AC-39)", func(t *testing.T) {
		e := cleanEnv(t)
		out := filepath.Join(t.TempDir(), "body")
		testenv.FakeBin(t, "savenotify", "cat > "+out)
		os.MkdirAll(e.cfgDir, 0o755)
		os.WriteFile(filepath.Join(e.cfgDir, "config.toml"), []byte("notify_command = \"savenotify\"\n"), 0o644)
		e.a.platform = withStatfs(e.a.platform.(fakePlatform), func(string) platform.FSUsage {
			return platform.FSUsage{ID: "fs1", Used: 10, Avail: 90}
		})
		// A directory where the history lock file goes makes only the save
		// fail; loading the history at the start of the run still works.
		os.MkdirAll(filepath.Join(e.a.platform.StateDir(), "history.json.lock", "x"), 0o755)
		if code := e.run("clean", "--yes", "--quiet"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(e.stdout.String(), "Deleted 4") {
			t.Errorf("stdout lacks the outcome summary: %q", e.stdout.String())
		}
		b, err := os.ReadFile(out)
		if err != nil || !strings.Contains(string(b), "Deleted 4") || !strings.Contains(string(b), "saving history") {
			t.Errorf("notification body = %q (err %v)", b, err)
		}
	})

	t.Run("a failed deletion exits 2", func(t *testing.T) {
		e := cleanEnv(t)
		inner := e.a.newExecutor
		e.a.newExecutor = func(rv map[string]func(context.Context) classify.Decision) *remove.Executor {
			x := inner(rv)
			x.DeleteTree = func(string) error { return os.ErrPermission }
			return x
		}
		if code := e.run("clean", "--yes"); code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
	})
}

func withStatfs(p fakePlatform, f func(string) platform.FSUsage) fakePlatform {
	p.statfs = f
	return p
}
