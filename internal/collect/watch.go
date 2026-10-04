package collect

import (
	"context"
	"os"
	"path/filepath"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
)

const watchCategory = "watch"

// Watch reports user-configured paths and globs (config.Watch). Every existing
// match is a manual candidate: files get `rm <path>`, directories
// `rm -rf <path>` (a plain rm refuses directories). Entries that match nothing
// are ignored, not errors, and an empty list yields an empty Result. A leading
// "~" expands to Home and a relative entry is relative to Home. Nothing is
// ever deleted by the collector.
type Watch struct {
	Config config.Config
	Home   string
}

var _ Collector = (*Watch)(nil)

// Name returns the collector's category name.
func (*Watch) Name() string { return watchCategory }

// Collect expands the watch entries into manual candidates.
func (w *Watch) Collect(context.Context) Result {
	var res Result
	for _, pattern := range w.Config.Watch {
		pattern = config.ExpandPath(pattern, w.Home, w.Home)
		matches, err := filepath.Glob(pattern)
		if err != nil {
			res.Warnings = append(res.Warnings, "watch: bad pattern "+pattern+": "+err.Error())
			continue
		}
		for _, m := range matches {
			fi, err := os.Lstat(m)
			if err != nil {
				continue
			}
			cmd, size := "rm "+m, fi.Size()
			if fi.IsDir() {
				cmd, size = "rm -rf "+m, dirSize(m)
			}
			res.Candidates = append(res.Candidates, classify.Candidate{
				Category: classify.CategoryWatch, Tier: classify.TierManual,
				Path: m, Size: size, Reason: "watched path", ReclaimCmd: cmd,
			})
		}
	}
	return res
}
