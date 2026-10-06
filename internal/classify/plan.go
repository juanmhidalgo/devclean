package classify

import "fmt"

// FSStat is a df-like filesystem stat: reserved blocks are in neither field.
type FSStat struct {
	Used  uint64
	Avail uint64
}

// PlanOptions are the inputs that shape the deletion set.
type PlanOptions struct {
	TTY bool
	Yes bool
	// Selection holds 1-based stale item numbers chosen at the prompt.
	Selection []int
}

// PlanResult is the deletion set plus notices for the user.
type PlanResult struct {
	Delete  []Candidate
	Notices []string
}

// DiskPercent returns used/(used+avail)*100, matching df.
func DiskPercent(used, avail uint64) float64 {
	total := used + avail
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// PlanClean decides what to delete. Caches kept because their filesystem is
// below pressure (or could not be stat'ed) get one notice per filesystem, so
// the report's reclaimable total is not mistaken for what clean deletes.
func PlanClean(candidates []Candidate, fsStats map[string]FSStat, pressure float64, opts PlanOptions) PlanResult {
	var res PlanResult
	selected := make(map[int]bool, len(opts.Selection))
	for _, n := range opts.Selection {
		selected[n] = true
	}
	staleBlocked := false
	staleN := 0
	var keptFS []string // FSIDs holding kept caches, in first-seen order
	kept := map[string]bool{}
	for _, c := range candidates {
		switch c.Tier {
		case TierGarbage:
			res.Delete = append(res.Delete, c)
		case TierCaches:
			st, ok := fsStats[c.FSID]
			if ok && DiskPercent(st.Used, st.Avail) >= pressure {
				res.Delete = append(res.Delete, c)
			} else if !kept[c.FSID] {
				kept[c.FSID] = true
				keptFS = append(keptFS, c.FSID)
			}
		case TierStale:
			staleN++
			switch {
			case opts.Yes:
				res.Delete = append(res.Delete, c)
			case !opts.TTY:
				staleBlocked = true
			case selected[staleN]:
				res.Delete = append(res.Delete, c)
			}
		}
	}
	for _, id := range keptFS {
		if st, ok := fsStats[id]; ok {
			res.Notices = append(res.Notices, fmt.Sprintf("caches kept: their filesystem is at %.1f%%, below the %.0f%% pressure threshold",
				DiskPercent(st.Used, st.Avail), pressure))
		} else {
			res.Notices = append(res.Notices, "caches kept: their filesystem's usage is unknown")
		}
	}
	if staleBlocked {
		res.Notices = append(res.Notices, "stale items skipped: not a TTY; pass --yes to delete them")
	}
	return res
}
