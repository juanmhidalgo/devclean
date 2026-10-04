package collect

import (
	"context"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/platform"
)

const systemCategory = "system"

// System reports system-level reclaim opportunities obtained from the
// platform. Everything is manual: devclean only prints the exact command (some
// need sudo) and never runs it. What the items are and how they are found is
// the platform's business; this collector only converts them.
type System struct {
	// Items is typically Platform.SystemItems.
	Items func(ctx context.Context) ([]platform.SystemItem, []string)
}

var _ Collector = (*System)(nil)

// Name returns the collector's category name.
func (*System) Name() string { return systemCategory }

// Collect converts platform items to manual candidates and skip reasons to Skips.
func (s *System) Collect(ctx context.Context) Result {
	var res Result
	items, skips := s.Items(ctx)
	for _, it := range items {
		res.Candidates = append(res.Candidates, classify.Candidate{
			Category: classify.CategorySystem, Tier: classify.TierManual,
			Path: it.Name, Size: it.Size, Reason: it.Reason, ReclaimCmd: it.ReclaimCmd,
		})
	}
	for _, r := range skips {
		res.Skipped = append(res.Skipped, Skip{Collector: systemCategory, Reason: r})
	}
	return res
}
