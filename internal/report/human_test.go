package report

import (
	"bytes"
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
