// Package report renders scan and clean results for people and machines.
package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/remove"
)

// Report is the model shared by the report renderers.
type Report struct {
	Candidates []classify.Candidate
	Skipped    []collect.Skip
	Warnings   []string
	Notices    []string
	// Outcomes and Freed are set only by clean; Freed non-nil marks a clean
	// document even when nothing was freed.
	Outcomes []remove.Outcome
	Freed    map[string]int64
	// Now is the reference time for "days ago".
	Now time.Time
}

var tierOrder = []struct {
	tier  classify.Tier
	title string
}{
	{classify.TierGarbage, "Garbage"},
	{classify.TierCaches, "Caches"},
	{classify.TierStale, "Stale"},
	{classify.TierManual, "Manual"},
}

var signalNames = map[classify.Signal]string{
	classify.SignalCommit:        "commit",
	classify.SignalHead:          "HEAD",
	classify.SignalIndex:         "index",
	classify.SignalArtifactMTime: "artifact mtime",
	classify.SignalMarkerATime:   "marker atime",
	classify.SignalHistory:       "history",
	classify.SignalImageLastSeen: "image last seen",
	classify.SignalFirstSeen:     "first seen",
}

// RenderHuman writes the plain-text report. Stale items are numbered 1-based
// in input order, matching classify.PlanClean's selection numbering.
func RenderHuman(w io.Writer, r Report) error {
	ew := &errWriter{w: w}
	for _, t := range tierOrder {
		var total int64
		var items []classify.Candidate
		for _, c := range r.Candidates {
			if c.Tier == t.tier {
				items = append(items, c)
				total += c.Size
			}
		}
		if len(items) == 0 {
			ew.printf("%s: nothing to reclaim\n\n", t.title)
			continue
		}
		ew.printf("%s (%s)\n", t.title, formatSize(total))
		for i, c := range items {
			prefix := "  "
			if t.tier == classify.TierStale {
				prefix = fmt.Sprintf("  [%d] ", i+1)
			}
			ew.printf("%s%10s  %s  last use: %s  %s\n", prefix, formatSize(c.Size), displayName(c), formatLastUse(c.LastUse, r.Now), c.Reason)
			if t.tier == classify.TierManual && c.ReclaimCmd != "" {
				ew.printf("      run: %s\n", c.ReclaimCmd)
			}
		}
		ew.printf("\n")
	}
	writeTrailer(ew, r)
	return ew.err
}

// writeTrailer writes the skipped collectors, warnings and notices that
// follow the candidates in every human-readable rendering.
func writeTrailer(ew *errWriter, r Report) {
	if len(r.Skipped) > 0 {
		ew.printf("Skipped collectors\n")
		for _, s := range r.Skipped {
			ew.printf("  %s: %s\n", s.Collector, s.Reason)
		}
		ew.printf("\n")
	}
	if len(r.Warnings) > 0 {
		ew.printf("Warnings\n")
		for _, m := range r.Warnings {
			ew.printf("  %s\n", m)
		}
		ew.printf("\n")
	}
	if len(r.Notices) > 0 {
		ew.printf("Notices\n")
		for _, m := range r.Notices {
			ew.printf("  %s\n", m)
		}
		ew.printf("\n")
	}
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, a ...any) {
	if e.err == nil {
		_, e.err = fmt.Fprintf(e.w, format, a...)
	}
}

func displayName(c classify.Candidate) string {
	if c.Category == classify.CategoryDocker {
		if id, ok := strings.CutPrefix(c.Path, "sha256:"); ok {
			if len(id) > 12 {
				id = id[:12]
			}
			return id
		}
	}
	return c.Path
}

func formatLastUse(u classify.LastUse, now time.Time) string {
	if u.At.IsZero() {
		return "-"
	}
	s := u.At.Format("2006-01-02")
	days := int(now.Sub(u.At).Hours() / 24)
	if days < 0 {
		days = 0
	}
	s += fmt.Sprintf(" (%d days ago", days)
	if n, ok := signalNames[u.Source]; ok {
		s += ", " + n
	}
	return s + ")"
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	for _, u := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		f /= unit
		if f < unit || u == "PiB" {
			return fmt.Sprintf("%.1f %s", f, u)
		}
	}
	return ""
}
