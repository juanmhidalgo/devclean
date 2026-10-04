package remove

import "github.com/juanmhidalgo/devclean/internal/platform"

// MeasureFreed reports, per filesystem ID, how many bytes became available
// between two statfs snapshots (after.Avail - before.Avail). The statfs
// delta is the truth: summing item sizes over-counts shared image layers
// and hard-linked files. The value is signed because available space can
// drop when something else writes meanwhile. A filesystem present in only
// one snapshot has no delta and is omitted.
func MeasureFreed(before, after map[string]platform.FSUsage) map[string]int64 {
	freed := make(map[string]int64, len(before))
	for id, b := range before {
		a, ok := after[id]
		if !ok {
			continue
		}
		freed[id] = int64(a.Avail) - int64(b.Avail)
	}
	return freed
}
