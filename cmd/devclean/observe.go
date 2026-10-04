package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
)

// walkedProjects is the history.Walked key pacing the project tree walk.
const walkedProjects = "projects"

// runObserve records observations and nothing else: it deletes nothing and
// takes no clean lock. Only docker and projects produce observations (the
// other collectors have no history), so only those run: docker every time,
// projects (the expensive file-tree walk) only once ObserveInterval has passed
// since the last walk. Coverage is saved only for collectors that ran, so a
// docker-only run prunes no project entries.
func (a *app) runObserve(ctx context.Context, opts runOptions) int {
	walked := false
	opts.choose = func(cfg config.Config, h history.History) []string {
		last := h.Walked[walkedProjects]
		if last.IsZero() || a.now().Sub(last) >= cfg.ObserveInterval.D() {
			walked = true
			return []string{"docker", walkedProjects}
		}
		return []string{"docker"}
	}
	s, code := a.scan(ctx, opts)
	if code != 0 {
		return code
	}
	h := applyObservations(s.hist, s.observations)
	for _, k := range s.skipped {
		if k.Collector == walkedProjects {
			walked = false
		}
	}
	// A --root walk covers only part of the configured roots, so it does not
	// count as the daily walk.
	if walked && len(opts.roots) == 0 {
		h.Walked = map[string]time.Time{}
		for k, v := range s.hist.Walked {
			h.Walked[k] = v
		}
		h.Walked[walkedProjects] = a.now()
	}
	if hw, err := history.Save(filepath.Join(a.platform.StateDir(), "history.json"), h, a.now, s.coverage...); err != nil {
		return a.fatal(err)
	} else if hw != "" {
		s.warnings = append(s.warnings, hw)
	}
	for _, w := range s.warnings {
		fmt.Fprintln(a.stderr, "devclean: warning:", w)
	}
	fmt.Fprintf(a.stdout, "Recorded %d observations.\n", len(s.observations))
	return exitCode(runOutcome{SkippedCollectors: len(s.skipped)})
}
