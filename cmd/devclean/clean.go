package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/lockfile"
	"github.com/juanmhidalgo/devclean/internal/notify"
	"github.com/juanmhidalgo/devclean/internal/platform"
	"github.com/juanmhidalgo/devclean/internal/remove"
	"github.com/juanmhidalgo/devclean/internal/report"
)

// cleanOptions are the flags specific to clean.
type cleanOptions struct {
	runOptions
	dryRun bool
	yes    bool
	quiet  bool
}

// maxPromptTries bounds the stale prompt re-asks on invalid input.
const maxPromptTries = 3

func (a *app) tty() bool { return a.isTTY != nil && a.isTTY() }

// runClean is the only destructive command. A real run holds clean.lock for
// its whole duration; --dry-run takes no lock and writes nothing (no history,
// no notification) but follows the same plan, prompts included.
func (a *app) runClean(ctx context.Context, opts cleanOptions) int {
	if code := a.preflight(); code != 0 {
		return code
	}
	stateDir := a.platform.StateDir()
	if !opts.dryRun {
		unlock, err := lockfile.TryLock(filepath.Join(stateDir, "clean.lock"))
		if errors.Is(err, lockfile.ErrLocked) {
			fmt.Fprintln(a.stderr, "devclean: another clean is running")
			return 1
		}
		if err != nil {
			return a.fatal(err)
		}
		defer unlock()
	}
	s, code := a.scan(ctx, opts.runOptions)
	if code != 0 {
		return code
	}

	fsPaths := map[string]string{} // FSID -> a filesystem path on it
	for _, c := range s.candidates {
		if _, ok := fsPaths[c.FSID]; !ok && c.FSID != "" && filepath.IsAbs(c.Path) {
			fsPaths[c.FSID] = c.Path
		}
	}
	before := a.statfsAll(fsPaths)
	stats := map[string]classify.FSStat{}
	above := false
	for id, u := range before {
		stats[id] = classify.FSStat{Used: u.Used, Avail: u.Avail}
		if classify.DiskPercent(u.Used, u.Avail) >= float64(s.cfg.PressurePercent) {
			above = true
		}
	}

	// --json is non-interactive: stale items need --yes.
	tty := a.tty() && !opts.json
	nStale := 0
	for _, c := range s.candidates {
		if c.Tier == classify.TierStale {
			nStale++
		}
	}
	base := report.Report{Candidates: s.candidates, Skipped: s.skipped, Warnings: s.warnings, Now: a.now()}
	var selection []int
	shown := false
	if tty && !opts.yes && nStale > 0 {
		if err := report.RenderHuman(a.stdout, base); err != nil {
			return a.fatal(err)
		}
		shown = true
		selection = a.askSelection(nStale)
	}
	plan := classify.PlanClean(s.candidates, stats, float64(s.cfg.PressurePercent),
		classify.PlanOptions{TTY: tty, Yes: opts.yes, Selection: selection})
	base.Notices = plan.Notices

	if opts.dryRun {
		return a.renderDryRun(opts, base, plan, shown, len(s.skipped))
	}

	newExec := a.newExecutor
	if newExec == nil {
		newExec = remove.NewExecutor
	}
	outcomes := newExec(s.revalidators).Run(ctx, plan.Delete)
	freed := remove.MeasureFreed(before, a.statfsAll(fsPaths))
	failed := 0
	for _, o := range outcomes {
		if o.Status == remove.StatusFailed {
			failed++
		}
	}

	// Deletions already happened: a history failure must not hide them, so
	// the report and the notification still go out and the exit code is 1.
	var saveErr error
	if hw, err := history.Save(filepath.Join(stateDir, "history.json"), applyObservations(s.hist, s.observations), a.now, s.coverage...); err != nil {
		saveErr = fmt.Errorf("saving history: %w", err)
		fmt.Fprintln(a.stderr, "devclean:", saveErr)
		base.Warnings = append(base.Warnings, saveErr.Error())
	} else if hw != "" {
		base.Warnings = append(base.Warnings, hw)
	}

	base.Outcomes, base.Freed = outcomes, freed
	if base.Freed == nil {
		base.Freed = map[string]int64{}
	}
	summary := summarize(outcomes, freed)
	if saveErr != nil {
		summary += "  " + saveErr.Error() + "\n"
	}
	render := report.RenderHuman
	if opts.json {
		render = report.RenderJSON
	}
	if err := render(a.stdout, base); err != nil {
		return a.fatal(err)
	}
	if !opts.json {
		fmt.Fprint(a.stdout, summary)
	}

	out := runOutcome{Fatal: saveErr, SkippedCollectors: len(s.skipped), FailedDeletions: failed, AbovePressure: above}
	if notify.ShouldNotify(out.summary(), opts.quiet) {
		if err := notify.Send(ctx, s.cfg.NotifyCommand, summary); err != nil {
			fmt.Fprintln(a.stderr, "devclean: notification failed:", err)
			out.NotifyErr = err
		}
	}
	return exitCode(out)
}

// renderDryRun prints the report and exactly the list clean would execute.
// JSON carries that list as its candidates.
func (a *app) renderDryRun(opts cleanOptions, r report.Report, plan classify.PlanResult, shown bool, skipped int) int {
	if opts.json {
		r.Candidates = plan.Delete
		if err := report.RenderJSON(a.stdout, r); err != nil {
			return a.fatal(err)
		}
		return exitCode(runOutcome{SkippedCollectors: skipped})
	}
	if !shown || len(r.Notices) > 0 {
		if err := report.RenderHuman(a.stdout, r); err != nil {
			return a.fatal(err)
		}
	}
	fmt.Fprintf(a.stdout, "Would delete (%d):\n", len(plan.Delete))
	for _, c := range plan.Delete {
		fmt.Fprintf(a.stdout, "  %s\n", c.Path)
	}
	return exitCode(runOutcome{SkippedCollectors: skipped})
}

// statfsAll stats one path per filesystem; a filesystem that cannot be
// stat'ed is left out, so its caches are not treated as under pressure.
func (a *app) statfsAll(paths map[string]string) map[string]platform.FSUsage {
	out := make(map[string]platform.FSUsage, len(paths))
	for id, p := range paths {
		if u, err := a.platform.Statfs(p); err == nil {
			out[id] = u
		}
	}
	return out
}

// askSelection prompts for which stale items to delete. EOF or too many
// invalid answers mean none.
func (a *app) askSelection(n int) []int {
	if a.stdin == nil {
		return nil
	}
	in := bufio.NewReader(a.stdin)
	for i := 0; i < maxPromptTries; i++ {
		fmt.Fprint(a.stdout, "Delete which stale items? [all/none/1,3-5]: ")
		line, err := in.ReadString('\n')
		if err != nil && (err != io.EOF || strings.TrimSpace(line) == "") {
			fmt.Fprintln(a.stdout)
			return nil
		}
		sel, perr := report.ParseSelection(line, n)
		if perr == nil {
			return sel
		}
		fmt.Fprintln(a.stderr, "devclean:", perr)
	}
	return nil
}

// applyObservations returns h plus the observations as history entries. A
// normal observation sets last_used (max) and first_seen (min); a
// FirstSeenOnly one sets first_seen only and never touches last_used.
func applyObservations(h history.History, obs []collect.Observation) history.History {
	out := h
	out.Entries = make(map[string]history.Entry, len(h.Entries)+len(obs))
	for k, e := range h.Entries {
		out.Entries[k] = e
	}
	if out.Version == 0 {
		out.Version = history.Version
	}
	for _, o := range obs {
		e := out.Entries[o.Key]
		if e.FirstSeen.IsZero() || o.At.Before(e.FirstSeen) {
			e.FirstSeen = o.At
		}
		if !o.FirstSeenOnly && o.At.After(e.LastUsed) {
			e.LastUsed = o.At
		}
		out.Entries[o.Key] = e
	}
	return out
}

// summarize is the human clean summary, also the notification body.
func summarize(outcomes []remove.Outcome, freed map[string]int64) string {
	var del, skip, fail int
	var b strings.Builder
	for _, o := range outcomes {
		switch o.Status {
		case remove.StatusDeleted:
			del++
		case remove.StatusSkipped:
			skip++
		case remove.StatusFailed:
			fail++
			fmt.Fprintf(&b, "  failed: %s: %v\n", o.Candidate.Path, o.Err)
		}
	}
	head := fmt.Sprintf("Deleted %d, skipped %d, failed %d\n", del, skip, fail)
	ids := make([]string, 0, len(freed))
	for id := range freed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		head += fmt.Sprintf("  freed on %s: %d bytes\n", id, freed[id])
	}
	return head + b.String()
}
