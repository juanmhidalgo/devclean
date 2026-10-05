package report

import (
	"bytes"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
)

func render(t *testing.T, r Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := RenderHuman(&buf, r); err != nil {
		t.Fatalf("RenderHuman: %v", err)
	}
	return buf.String()
}

func TestRenderHuman(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	last := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	full := Report{
		Now: now,
		Candidates: []classify.Candidate{
			{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/first/node_modules", Size: 3 << 30, Reason: "unused", LastUse: classify.LastUse{At: last, Source: classify.SignalCommit}},
			{Category: classify.CategoryCaches, Tier: classify.TierCaches, Path: "/c/npm", Size: 1536 << 20, Reason: "cache dir"},
			{Category: classify.CategoryProjects, Tier: classify.TierGarbage, Path: "/g/tmp", Size: 2048, Reason: "garbage rule"},
			{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: "docker builder prune", Size: 5 << 20, Reason: "build cache", ReclaimCmd: "docker builder prune -f"},
			{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/second/.venv", Size: 100, Reason: "old venv"},
			{Category: classify.CategoryDocker, Tier: classify.TierStale, Path: "sha256:0123456789abcdef0123456789abcdef", Size: 10, Reason: "old image"},
		},
		Skipped:  []collect.Skip{{Collector: "docker", Reason: "daemon unreachable"}},
		Warnings: []string{"warn-one"},
		Notices:  []string{"notice-one"},
	}

	t.Run("tier order garbage caches stale manual", func(t *testing.T) {
		out := render(t, full)
		prev := -1
		for _, m := range []string{"/g/tmp", "/c/npm", "/p/first/node_modules", "docker builder prune"} {
			i := strings.Index(out, m)
			if i < 0 || i < prev {
				t.Fatalf("%q missing or out of order (idx %d, prev %d):\n%s", m, i, prev, out)
			}
			prev = i
		}
	})

	t.Run("line fields", func(t *testing.T) {
		out := render(t, full)
		for _, m := range []string{"3.0 GiB", "1.5 GiB", "2.0 KiB", "2026-03-15", "203 days ago", "commit", "unused", "cache dir"} {
			if !strings.Contains(out, m) {
				t.Errorf("missing %q:\n%s", m, out)
			}
		}
	})

	t.Run("stale numbered in input order", func(t *testing.T) {
		out := render(t, full)
		for n, m := range []string{"/p/first/node_modules", "/p/second/.venv", "0123456789ab"} {
			var line string
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, m) {
					line = l
				}
			}
			want := "[" + string(rune('1'+n)) + "]"
			if !strings.Contains(line, want) {
				t.Errorf("line for %q lacks %s: %q", m, want, line)
			}
		}
		if strings.Contains(out, "0123456789abcdef") {
			t.Errorf("image ID not shortened:\n%s", out)
		}
	})

	t.Run("manual prints reclaim command", func(t *testing.T) {
		if out := render(t, full); !strings.Contains(out, "docker builder prune -f") {
			t.Errorf("missing reclaim cmd:\n%s", out)
		}
	})

	t.Run("empty tier says nothing to reclaim", func(t *testing.T) {
		out := render(t, Report{Now: now, Candidates: full.Candidates[:1]})
		if got := strings.Count(out, "nothing to reclaim"); got != 3 {
			t.Errorf("want 3 empty tiers, got %d:\n%s", got, out)
		}
	})

	t.Run("skipped warnings notices", func(t *testing.T) {
		out := render(t, full)
		for _, m := range []string{"docker", "daemon unreachable", "warn-one", "notice-one"} {
			if !strings.Contains(out, m) {
				t.Errorf("missing %q:\n%s", m, out)
			}
		}
	})
}

func TestRenderHumanGroups(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	anon := strings.Repeat("ab", 32)
	vol := func(name string) classify.Candidate {
		return classify.Candidate{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: name,
			Reason: collect.ReasonUnusedVolume, ReclaimCmd: "docker volume rm " + name, SizeUnknown: true}
	}
	snap := func(name string, size int64) classify.Candidate {
		return classify.Candidate{Category: classify.CategorySystem, Tier: classify.TierManual, Path: name, Size: size,
			Reason: "disabled snap revision", ReclaimCmd: "sudo snap remove " + name}
	}
	r := Report{Now: now, Home: "/home/u", Candidates: []classify.Candidate{
		vol("pgdata"), vol(anon), vol(strings.Repeat("cd", 32)),
		{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: strings.Repeat("ef", 32), Size: 7,
			Reason: collect.ReasonUsedVolume, ReclaimCmd: "docker volume rm " + strings.Repeat("ef", 32)},
		{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: "appdata", Size: 9, UsedBy: []string{"c1", "c2", "c3"},
			Reason: collect.ReasonUsedVolume, ReclaimCmd: "docker volume rm appdata"},
		snap("small", 1<<20), snap("big", 2<<30),
		{Category: classify.CategoryCaches, Tier: classify.TierManual, Path: "/home/u/.yarn/berry/cache", Size: 3 << 30,
			Reason: "yarn berry cache", ReclaimCmd: "rm -rf /home/u/.yarn/berry/cache"},
		{Category: classify.CategoryCaches, Tier: classify.TierCaches, Path: "/home/u/.cache/go-build", Size: 1 << 30, Reason: "Go build cache"},
		{Category: classify.CategoryCaches, Tier: classify.TierGarbage, Path: "uv cache prune", Reason: "tool-native prune", ReclaimCmd: "uv cache prune", SizeUnknown: true},
		{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/a", Size: 1, Reason: "old"},
		{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/b", Size: 5, Reason: "old"},
	}}
	out := render(t, r)

	t.Run("reason printed once per group", func(t *testing.T) {
		for _, m := range []string{collect.ReasonUnusedVolume, "disabled snap revision"} {
			if n := strings.Count(out, m); n != 1 {
				t.Errorf("%q printed %d times:\n%s", m, n, out)
			}
		}
	})
	t.Run("anonymous volumes fold into one line", func(t *testing.T) {
		if strings.Contains(out, anon) || !strings.Contains(out, "2 anonymous volumes (names in --json); `docker volume prune`") {
			t.Errorf("anonymous volumes not folded:\n%s", out)
		}
		// prune never removes a volume a container uses: no hint there.
		if n := strings.Count(out, "docker volume prune"); n != 1 {
			t.Errorf("prune hint printed %d times, want only for unused volumes:\n%s", n, out)
		}
		if !strings.Contains(out, "docker volume rm pgdata") {
			t.Errorf("named volume missing:\n%s", out)
		}
	})
	t.Run("a volume containers use shows them, not the command", func(t *testing.T) {
		if strings.Contains(out, "docker volume rm appdata") || !strings.Contains(out, "appdata") ||
			!strings.Contains(out, "used by c1, c2 +1 more") {
			t.Errorf("used volume rendered wrong:\n%s", out)
		}
	})
	t.Run("unmeasured sizes are not shown as 0 B", func(t *testing.T) {
		if strings.Contains(out, "0 B") {
			t.Errorf("unmeasured size rendered as 0 B:\n%s", out)
		}
		for _, m := range []string{"Garbage — 1 item, size not measured", "3 items, size not measured", "(3 not measured)"} {
			if !strings.Contains(out, m) {
				t.Errorf("missing %q:\n%s", m, out)
			}
		}
	})
	t.Run("home shortened to tilde", func(t *testing.T) {
		if strings.Contains(out, "/home/u") || !strings.Contains(out, "~/.yarn/berry/cache") || !strings.Contains(out, "~/.cache/go-build") {
			t.Errorf("home not shortened:\n%s", out)
		}
	})
	t.Run("singletons first then groups largest first", func(t *testing.T) {
		prev := -1
		for _, m := range []string{"~/.yarn/berry/cache", "snap remove big", "snap remove small", "pgdata"} {
			i := strings.Index(out, m)
			if i < 0 || i < prev {
				t.Fatalf("%q missing or out of order:\n%s", m, out)
			}
			prev = i
		}
	})
	t.Run("stale keeps input order and numbering", func(t *testing.T) {
		a, b := strings.Index(out, "[1]"), strings.Index(out, "[2]")
		if a < 0 || b < a || !strings.Contains(out[a:b], "/p/a") || !strings.Contains(out[b:], "/p/b") {
			t.Errorf("stale order changed:\n%s", out)
		}
	})
}

// Colors only add escapes: stripped, the colored output is the plain one, so
// alignment never depends on whether the terminal shows color.
func TestRenderColorOnlyAddsEscapes(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	r := Report{Now: now, Home: "/home/u", Candidates: []classify.Candidate{
		{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/a", Size: 2 << 30, Reason: "old",
			LastUse: classify.LastUse{At: now.AddDate(0, -6, 0), Source: classify.SignalCommit}},
		{Category: classify.CategoryCaches, Tier: classify.TierCaches, Path: "/home/u/.cache/x", Size: 5 << 20, Reason: "x cache"},
		{Category: classify.CategoryCaches, Tier: classify.TierGarbage, Path: "uv cache prune", Reason: "tool-native prune", ReclaimCmd: "uv cache prune", SizeUnknown: true},
		{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: "v1", Size: 10, Reason: "vol", ReclaimCmd: "docker volume rm v1"},
		{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: "v2", Size: 20, Reason: "vol", ReclaimCmd: "docker volume rm v2"},
	}, Warnings: []string{"w"}, Skipped: []collect.Skip{{Collector: "docker", Reason: "x"}}}
	strip := regexp.MustCompile("\x1b\\[[0-9;]*m")
	for name, fn := range map[string]func(io.Writer, Report) error{"human": RenderHuman, "summary": RenderSummary} {
		var plain, colored bytes.Buffer
		if err := fn(&plain, r); err != nil {
			t.Fatal(err)
		}
		r.Color = true
		if err := fn(&colored, r); err != nil {
			t.Fatal(err)
		}
		r.Color = false
		if !strings.Contains(colored.String(), "\x1b[") {
			t.Errorf("%s: no escapes with Color on", name)
		}
		if strings.Contains(plain.String(), "\x1b[") {
			t.Errorf("%s: escapes with Color off", name)
		}
		if got := strip.ReplaceAllString(colored.String(), ""); got != plain.String() {
			t.Errorf("%s: stripped colored output differs:\n%s\n---\n%s", name, got, plain.String())
		}
	}
}
