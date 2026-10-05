package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
)

func TestRenderSummary(t *testing.T) {
	r := Report{
		Candidates: []classify.Candidate{
			{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/a/node_modules", Size: 3 << 30},
			{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/b/node_modules", Size: 1 << 30},
			{Category: classify.CategoryCaches, Tier: classify.TierCaches, Path: "/c/npm", Size: 512 << 20},
			{Category: classify.CategoryDocker, Tier: classify.TierGarbage, Path: "sha256:abc", Size: 512 << 20},
			{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: "pgdata", Size: 7 << 30},
		},
		Skipped: []collect.Skip{{Collector: "system", Reason: "snap failed"}},
	}
	var buf bytes.Buffer
	if err := RenderSummary(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"Garbage      1 item   512.0 MiB\n",
		"Caches       1 item   512.0 MiB\n",
		"Stale        2 items    4.0 GiB\n",
		"Manual       1 item     7.0 GiB\n",
		// Manual items are not counted as reclaimable.
		"Reclaimable (garbage+caches+stale)  5.0 GiB\n",
		"  projects     2 items    4.0 GiB\n",
		"  docker       2 items    7.5 GiB\n",
		"  caches       1 item   512.0 MiB\n",
		"Skipped collectors\n  system: snap failed\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/p/a/node_modules") || strings.Contains(out, "venvs") {
		t.Errorf("summary lists items or empty categories:\n%s", out)
	}
	if strings.Index(out, "projects") > strings.Index(out, "docker") {
		t.Errorf("categories out of order:\n%s", out)
	}
}

func TestRenderSummaryEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderSummary(&buf, Report{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Garbage      0 items        0 B\n") || strings.Contains(out, "By category") {
		t.Errorf("empty summary:\n%s", out)
	}
}
