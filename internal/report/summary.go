package report

import (
	"fmt"
	"io"

	"github.com/juanmhidalgo/devclean/internal/classify"
)

// categoryOrder is the order categories are listed in the summary.
var categoryOrder = []classify.Category{
	classify.CategoryProjects, classify.CategoryDocker, classify.CategoryVenvs,
	classify.CategoryCaches, classify.CategorySystem, classify.CategoryWatch,
}

// RenderSummary writes only the totals: count and size per tier, what
// garbage, caches and stale add up to, and the same per category. Skipped
// collectors, warnings and notices still follow, since they say the totals
// may be incomplete.
func RenderSummary(w io.Writer, r Report) error {
	ew := &errWriter{w: w}
	st := style{on: r.Color}
	type total struct {
		n    int
		size int64
	}
	byTier := map[classify.Tier]total{}
	byCat := map[classify.Category]total{}
	var reclaimable int64
	for _, c := range r.Candidates {
		t := byTier[c.Tier]
		byTier[c.Tier] = total{t.n + 1, t.size + c.Size}
		k := byCat[c.Category]
		byCat[c.Category] = total{k.n + 1, k.size + c.Size}
		switch c.Tier {
		case classify.TierGarbage, classify.TierCaches, classify.TierStale:
			reclaimable += c.Size
		}
	}

	for _, t := range tierOrder {
		ew.printf("%s %5d %-5s %10s\n", st.tier(t.tier, fmt.Sprintf("%-8s", t.title)), byTier[t.tier].n, items(byTier[t.tier].n), FormatSize(byTier[t.tier].size))
	}
	ew.printf("%s\n", st.dim("------------------------------"))
	ew.printf("%s\n\n", st.bold("Reclaimable (garbage+caches+stale)  "+FormatSize(reclaimable)))

	if len(byCat) > 0 {
		ew.printf("%s\n", st.bold("By category"))
		for _, c := range categoryOrder {
			if t, ok := byCat[c]; ok {
				ew.printf("  %-8s %5d %-5s %10s\n", categoryNames[c], t.n, items(t.n), FormatSize(t.size))
			}
		}
		ew.printf("\n")
	}
	writeTrailer(ew, r)
	return ew.err
}

func items(n int) string {
	if n == 1 {
		return "item"
	}
	return "items"
}
