package main

import (
	"bytes"
	"context"
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
	"github.com/juanmhidalgo/devclean/internal/remove"
	"github.com/juanmhidalgo/devclean/internal/testenv"
)

var allCollectors = []string{"projects", "docker", "venvs", "caches", "system", "watch"}

// spyCollector records its Collect calls into a shared log.
type spyCollector struct {
	name string
	log  *[]string
	res  collect.Result
}

func (s spyCollector) Name() string { return s.name }
func (s spyCollector) Collect(context.Context) collect.Result {
	*s.log = append(*s.log, s.name)
	return s.res
}

type reportEnv struct {
	a       *app
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
	calls   []string
	deletes []string
	cfgSeen []config.Config
	cfgDir  string
}

// writeTestFile writes data to path, creating its parent directories.
func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	mkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func newReportEnv(t *testing.T, plat fakePlatform) *reportEnv {
	t.Helper()
	state, cfg := testenv.Isolate(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	plat.stateDir, plat.configDir = state, cfg
	plat.cacheDir, plat.dataDir = filepath.Join(home, "c"), filepath.Join(home, "d")
	e := &reportEnv{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, cfgDir: cfg}
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e.a = &app{
		platform: plat,
		stdout:   e.stdout,
		stderr:   e.stderr,
		now:      func() time.Time { return fixed },
		newCollectors: func(c config.Config, _ history.History) []collect.Collector {
			e.cfgSeen = append(e.cfgSeen, c)
			var out []collect.Collector
			for _, n := range allCollectors {
				res := collect.Result{}
				if n == "caches" {
					res.Candidates = []classify.Candidate{{Category: classify.CategoryCaches, Path: "/x/cache", Tier: classify.TierCaches, Size: 10}}
				}
				out = append(out, spyCollector{name: n, log: &e.calls, res: res})
			}
			return out
		},
		newExecutor: func(rv map[string]func(context.Context) classify.Decision) *remove.Executor {
			x := remove.NewExecutor(rv)
			x.DeleteTree = func(p string) error { e.deletes = append(e.deletes, p); return nil }
			x.DeleteImage = func(_ context.Context, id string, _ []string) error { e.deletes = append(e.deletes, id); return nil }
			x.RunAction = func(_ context.Context, cmd string) error { e.deletes = append(e.deletes, cmd); return nil }
			return x
		},
	}
	return e
}

func (e *reportEnv) run(args ...string) int {
	e.stdout.Reset()
	e.stderr.Reset()
	e.calls = nil
	return e.a.execute(args)
}

func TestReportCommand(t *testing.T) {
	t.Run("bare devclean prints the help and scans nothing (AC-1)", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		if code := e.run(); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		out := e.stdout.String()
		for _, want := range []string{"Usage:", "report", "clean", "observe", "schedule"} {
			if !strings.Contains(out, want) {
				t.Errorf("help lacks %q:\n%s", want, out)
			}
		}
		if len(e.calls) != 0 || len(e.deletes) != 0 {
			t.Errorf("bare devclean ran collectors %v / deleted %v", e.calls, e.deletes)
		}
	})

	t.Run("report deletes nothing (AC-1)", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		if code := e.run("report"); code != 0 {
			t.Fatalf("report exit = %d, stderr %q", code, e.stderr.String())
		}
		if !strings.Contains(e.stdout.String(), "/x/cache") {
			t.Fatalf("output lacks candidate:\n%s", e.stdout.String())
		}
		if len(e.deletes) != 0 {
			t.Errorf("deleter called: %v", e.deletes)
		}
	})

	t.Run("report --summary prints totals, not items", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		if code := e.run("report", "--summary"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		out := e.stdout.String()
		if !strings.Contains(out, "Reclaimable") || !strings.Contains(out, "By category") {
			t.Errorf("summary lacks totals:\n%s", out)
		}
		if strings.Contains(out, "/x/cache") {
			t.Errorf("summary lists items:\n%s", out)
		}
	})

	t.Run("--color decides escapes", func(t *testing.T) {
		tty := func() bool { return true }
		for _, tc := range []struct {
			name    string
			args    []string
			tty     func() bool
			noColor string
			want    bool
		}{
			{"auto on a pipe", nil, nil, "", false},
			{"auto on a terminal", nil, tty, "", true},
			{"auto honors NO_COLOR", nil, tty, "1", false},
			{"always on a pipe", []string{"--color", "always"}, nil, "", true},
			{"never on a terminal", []string{"--color", "never"}, tty, "", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv("NO_COLOR", tc.noColor)
				t.Setenv("TERM", "xterm")
				e := newReportEnv(t, fakePlatform{euid: 1000})
				e.a.stdoutIsTTY = tc.tty
				if code := e.run(append([]string{"report"}, tc.args...)...); code != 0 {
					t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
				}
				if got := strings.Contains(e.stdout.String(), "\x1b["); got != tc.want {
					t.Errorf("escapes = %v, want %v:\n%q", got, tc.want, e.stdout.String())
				}
			})
		}
		e := newReportEnv(t, fakePlatform{euid: 1000})
		if code := e.run("report", "--color", "sometimes"); code != 1 || len(e.calls) != 0 {
			t.Errorf("invalid --color: exit = %d, calls %v", code, e.calls)
		}
	})

	t.Run("report --summary with --json exits 1 before collecting", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		if code := e.run("report", "--summary", "--json"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(e.stderr.String(), "--summary") || len(e.calls) != 0 {
			t.Errorf("stderr %q, calls %v", e.stderr.String(), e.calls)
		}
	})

	t.Run("all six collectors run when --only absent (AC-4)", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		e.run("report")
		if !reflect.DeepEqual(e.calls, allCollectors) {
			t.Errorf("calls = %v, want %v", e.calls, allCollectors)
		}
	})

	t.Run("--only runs just the named collectors (AC-4)", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		if code := e.run("report", "--only", "docker", "--only", "venvs,system"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		if want := []string{"docker", "venvs", "system"}; !reflect.DeepEqual(e.calls, want) {
			t.Errorf("calls = %v, want %v", e.calls, want)
		}
		e.run("report", "--only", "watch")
		if want := []string{"watch"}; !reflect.DeepEqual(e.calls, want) {
			t.Errorf("single --only calls = %v, want %v", e.calls, want)
		}
	})

	t.Run("unknown --only name exits 1 naming it", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		if code := e.run("report", "--only", "bogus"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(e.stderr.String(), "bogus") || len(e.calls) != 0 {
			t.Errorf("stderr %q calls %v", e.stderr.String(), e.calls)
		}
	})

	t.Run("pin: --root replaces configured scan roots", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		writeTestFile(t, filepath.Join(e.cfgDir, "config.toml"), "[scan]\nroots = [\"/configured\"]\n")
		a, b := t.TempDir(), t.TempDir()
		e.run("report", "--root", a, "--root", b)
		e.run("report")
		if want := []string{a, b}; !reflect.DeepEqual(e.cfgSeen[0].Scan.Roots, want) {
			t.Errorf("roots with --root = %v, want %v", e.cfgSeen[0].Scan.Roots, want)
		}
		if want := []string{"/configured"}; !reflect.DeepEqual(e.cfgSeen[1].Scan.Roots, want) {
			t.Errorf("roots without --root = %v, want %v", e.cfgSeen[1].Scan.Roots, want)
		}
	})

	t.Run("relative --root and ~ or relative excludes reach the walker absolute", func(t *testing.T) {
		// MUTATION: see docs/mutation-checks.md (excludes vs relative roots).
		e := newReportEnv(t, fakePlatform{euid: 1000})
		home := os.Getenv("HOME")
		writeTestFile(t, filepath.Join(e.cfgDir, "config.toml"), "[scan]\nexcludes = [\"~/work/keep\", \"rel/keep\"]\n")
		work := filepath.Join(home, "work")
		mkdirAll(t, work)
		t.Chdir(work)
		e.run("report", "--root", ".", "--root", "~/other")
		got := e.cfgSeen[0].Scan
		if want := []string{work, filepath.Join(home, "other")}; !reflect.DeepEqual(got.Roots, want) {
			t.Errorf("roots = %v, want %v", got.Roots, want)
		}
		// A relative exclude in the config file is relative to home, not to
		// the directory devclean runs from.
		if want := []string{filepath.Join(home, "work/keep"), filepath.Join(home, "rel/keep")}; !reflect.DeepEqual(got.Excludes, want) {
			t.Errorf("excludes = %v, want %v", got.Excludes, want)
		}
	})

	t.Run("pin: invalid config exits 1 before any collector", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		writeTestFile(t, filepath.Join(e.cfgDir, "config.toml"), "this is = = not toml\n")
		if code := e.run("report"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if len(e.calls) != 0 || len(e.cfgSeen) != 0 || e.stdout.Len() != 0 {
			t.Errorf("collectors ran or output produced: calls %v stdout %q", e.calls, e.stdout.String())
		}
		if !strings.Contains(e.stderr.String(), "config.toml") {
			t.Errorf("stderr = %q, want it to name the config file", e.stderr.String())
		}
	})

	t.Run("pin: preflight runs first", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 0})
		writeTestFile(t, filepath.Join(e.cfgDir, "config.toml"), "garbage = = =\n")
		if code := e.run("report"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if got := e.stderr.String(); !strings.Contains(got, "refusing to run as root") || strings.Contains(got, "config.toml") {
			t.Errorf("stderr = %q, want only the preflight refusal", got)
		}
		if len(e.calls) != 0 {
			t.Errorf("collectors ran: %v", e.calls)
		}
	})

	t.Run("skipped collector exits 2", func(t *testing.T) {
		e := newReportEnv(t, fakePlatform{euid: 1000})
		inner := e.a.newCollectors
		e.a.newCollectors = func(c config.Config, h history.History) []collect.Collector {
			cs := inner(c, h)
			cs[1] = spyCollector{name: "docker", log: &e.calls, res: collect.Result{
				Skipped: []collect.Skip{{Collector: "docker", Reason: "daemon down"}}}}
			return cs
		}
		if code := e.run("report"); code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
		if !strings.Contains(e.stdout.String(), "daemon down") {
			t.Errorf("stdout lacks skip reason:\n%s", e.stdout.String())
		}
	})
}

func tierEnv(t *testing.T) *reportEnv {
	e := newReportEnv(t, fakePlatform{euid: 1000})
	e.a.newCollectors = func(config.Config, history.History) []collect.Collector {
		return []collect.Collector{spyCollector{name: "projects", log: &e.calls, res: collect.Result{
			Candidates: []classify.Candidate{
				{Category: classify.CategoryCaches, Path: "/t/garbage", Tier: classify.TierGarbage, Size: 1},
				{Category: classify.CategoryCaches, Path: "/t/caches", Tier: classify.TierCaches, Size: 2},
				{Category: classify.CategoryProjects, Path: "/t/stale", Tier: classify.TierStale, Size: 3},
				{Category: classify.CategoryCaches, Path: "/t/manual", Tier: classify.TierManual, Size: 4},
			}}}}
	}
	return e
}

func TestTierFilter(t *testing.T) {
	t.Run("filterTiers keeps order and nil keeps all (feeds the deletion plan)", checkFilterTiersHelper)

	t.Run("repeated --tier keeps only those tiers (AC-5)", func(t *testing.T) {
		e := tierEnv(t)
		if code := e.run("report", "--tier", "stale", "--tier", "manual"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		out := e.stdout.String()
		for _, want := range []string{"/t/stale", "/t/manual"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %s:\n%s", want, out)
			}
		}
		for _, no := range []string{"/t/garbage", "/t/caches"} {
			if strings.Contains(out, no) {
				t.Errorf("output has filtered-out %s:\n%s", no, out)
			}
		}
	})

	t.Run("comma list works (AC-5)", func(t *testing.T) {
		e := tierEnv(t)
		if code := e.run("report", "--tier", "garbage,caches"); code != 0 {
			t.Fatalf("exit = %d", code)
		}
		out := e.stdout.String()
		if !strings.Contains(out, "/t/garbage") || !strings.Contains(out, "/t/caches") ||
			strings.Contains(out, "/t/stale") || strings.Contains(out, "/t/manual") {
			t.Errorf("unexpected output:\n%s", out)
		}
	})

	t.Run("no --tier keeps every tier", func(t *testing.T) {
		e := tierEnv(t)
		e.run("report")
		for _, p := range []string{"/t/garbage", "/t/caches", "/t/stale", "/t/manual"} {
			if !strings.Contains(e.stdout.String(), p) {
				t.Errorf("output lacks %s", p)
			}
		}
	})

	t.Run("unknown --tier exits 1 naming it before collecting", func(t *testing.T) {
		e := tierEnv(t)
		if code := e.run("report", "--tier", "bogus"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(e.stderr.String(), "bogus") || len(e.calls) != 0 {
			t.Errorf("stderr %q calls %v", e.stderr.String(), e.calls)
		}
	})
}

func checkFilterTiersHelper(t *testing.T) {
	cands := []classify.Candidate{
		{Path: "a", Tier: classify.TierStale}, {Path: "b", Tier: classify.TierGarbage},
		{Path: "c", Tier: classify.TierStale}, {Path: "d", Tier: classify.TierManual},
	}
	keep, err := parseTiers([]string{"stale"})
	if err != nil {
		t.Fatal(err)
	}
	got := filterTiers(cands, keep)
	if len(got) != 2 || got[0].Path != "a" || got[1].Path != "c" {
		t.Errorf("filterTiers = %v, want a,c in order", got)
	}
	if all := filterTiers(cands, nil); len(all) != 4 {
		t.Errorf("nil set dropped candidates: %v", all)
	}
}
