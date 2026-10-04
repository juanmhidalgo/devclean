package classify

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

// PlanClean decides what to delete.
func PlanClean(candidates []Candidate, fsStats map[string]FSStat, pressure float64, opts PlanOptions) PlanResult {
	var res PlanResult
	selected := make(map[int]bool, len(opts.Selection))
	for _, n := range opts.Selection {
		selected[n] = true
	}
	staleBlocked := false
	staleN := 0
	for _, c := range candidates {
		switch c.Tier {
		case TierGarbage:
			res.Delete = append(res.Delete, c)
		case TierCaches:
			st, ok := fsStats[c.FSID]
			if ok && DiskPercent(st.Used, st.Avail) >= pressure {
				res.Delete = append(res.Delete, c)
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
	if staleBlocked {
		res.Notices = append(res.Notices, "stale items skipped: not a TTY; pass --yes to delete them")
	}
	return res
}
