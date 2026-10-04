package classify

import (
	"sort"
	"time"
)

// FilterScanBursts drops every read that falls inside a window (span <= window)
// shared by minArtifacts or more distinct artifacts, treating it as a scan
// rather than real use. Isolated reads are kept, in their original order.
//
// Each window starts at a read and spans [start, start+window]. The reads are
// visited in time order with two pointers and a per-artifact count, so the
// cost is O(N log N): a scan over thousands of node_modules packages yields
// one read per package.
func FilterScanBursts(reads []Read, minArtifacts int, window time.Duration) []Read {
	order := make([]int, len(reads))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return reads[order[a]].At.Before(reads[order[b]].At) })

	drop := make([]bool, len(reads))
	counts := map[string]int{}
	lo, hi := 0, 0 // order[lo:hi] is the current window
	dropped := 0   // order[:dropped] is already marked
	for i := range order {
		start := reads[order[i]].At
		// Reads tied with the start belong to its window, so only strictly
		// earlier ones leave it.
		for lo < hi && reads[order[lo]].At.Before(start) {
			id := reads[order[lo]].ArtifactID
			if counts[id]--; counts[id] == 0 {
				delete(counts, id)
			}
			lo++
		}
		limit := start.Add(window)
		for hi < len(order) && !reads[order[hi]].At.After(limit) {
			counts[reads[order[hi]].ArtifactID]++
			hi++
		}
		if len(counts) >= minArtifacts {
			for k := max(lo, dropped); k < hi; k++ {
				drop[order[k]] = true
			}
			dropped = max(dropped, hi)
		}
	}

	var kept []Read
	for i, r := range reads {
		if !drop[i] {
			kept = append(kept, r)
		}
	}
	return kept
}
