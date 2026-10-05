package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/report"
)

// collectorNames lists every valid --only value, in run order.
var collectorNames = []string{"projects", "docker", "venvs", "caches", "system", "watch"}

// runOptions are the flags shared by every command that scans.
type runOptions struct {
	only  []string
	roots []string
	tiers []string
	json  bool
	// choose, when set, picks the collectors once config and history are
	// loaded, overriding only.
	choose func(config.Config, history.History) []string
}

// scan is everything one run gathered, merged across collectors.
type scan struct {
	cfg          config.Config
	hist         history.History
	candidates   []classify.Candidate
	skipped      []collect.Skip
	warnings     []string
	coverage     []history.Coverage
	observations []collect.Observation
	revalidators map[string]func(context.Context) classify.Decision
}

// fatal prints the error and returns the exit code 1.
func (a *app) fatal(err error) int {
	fmt.Fprintln(a.stderr, "devclean:", err)
	return 1
}

// validateOnly rejects names that are not collectors.
func validateOnly(only []string) error {
	for _, n := range only {
		if !slices.Contains(collectorNames, n) {
			return fmt.Errorf("unknown --only category %q (valid: %s)", n, strings.Join(collectorNames, ", "))
		}
	}
	return nil
}

// tierNames maps every valid --tier value to its tier.
var tierNames = map[string]classify.Tier{
	"garbage": classify.TierGarbage,
	"caches":  classify.TierCaches,
	"stale":   classify.TierStale,
	"manual":  classify.TierManual,
}

// parseTiers validates --tier values; an unknown one is an error naming it.
func parseTiers(names []string) (map[classify.Tier]bool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	set := map[classify.Tier]bool{}
	for _, n := range names {
		t, ok := tierNames[n]
		if !ok {
			return nil, fmt.Errorf("unknown --tier %q (valid: garbage, caches, stale, manual)", n)
		}
		set[t] = true
	}
	return set, nil
}

// filterTiers keeps the candidates whose tier is in keep, preserving order. A
// nil set keeps everything.
func filterTiers(cands []classify.Candidate, keep map[classify.Tier]bool) []classify.Candidate {
	if keep == nil {
		return cands
	}
	var out []classify.Candidate
	for _, c := range cands {
		if keep[c.Tier] {
			out = append(out, c)
		}
	}
	return out
}

// loadConfig reads the user's config file, applying defaults.
func (a *app) loadConfig() (config.Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config.Config{}, err
	}
	return config.Load(filepath.Join(a.platform.ConfigDir(), "config.toml"), home)
}

// scan runs the shared front half of every command: preflight, flag
// validation, config, history, and the selected collectors. A non-zero code
// means the run must stop with it, and no collector has been called when the
// failure precedes collection.
func (a *app) scan(ctx context.Context, opts runOptions) (*scan, int) {
	if code := a.preflight(); code != 0 {
		return nil, code
	}
	if err := validateOnly(opts.only); err != nil {
		return nil, a.fatal(err)
	}
	keep, err := parseTiers(opts.tiers)
	if err != nil {
		return nil, a.fatal(err)
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, a.fatal(err)
	}
	if len(opts.roots) > 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, a.fatal(err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			return nil, a.fatal(err)
		}
		cfg.Scan.Roots = config.ExpandPaths(opts.roots, home, cwd)
	}
	hist, warn, err := history.Load(filepath.Join(a.platform.StateDir(), "history.json"), a.now)
	if err != nil {
		return nil, a.fatal(err)
	}
	s := &scan{cfg: cfg, hist: hist, revalidators: map[string]func(context.Context) classify.Decision{}}
	if warn != "" {
		s.warnings = append(s.warnings, warn)
	}
	if opts.choose != nil {
		opts.only = opts.choose(cfg, hist)
	}
	for _, c := range a.newCollectors(cfg, hist) {
		if len(opts.only) > 0 && !slices.Contains(opts.only, c.Name()) {
			continue
		}
		res := c.Collect(ctx)
		s.candidates = append(s.candidates, res.Candidates...)
		s.skipped = append(s.skipped, res.Skipped...)
		s.warnings = append(s.warnings, res.Warnings...)
		s.coverage = append(s.coverage, res.Coverage...)
		s.observations = append(s.observations, res.Observations...)
		for k, v := range res.Revalidators {
			s.revalidators[k] = v
		}
	}
	s.candidates = filterTiers(s.candidates, keep)
	return s, 0
}

// runReport is the read-only default: it scans and renders, never deleting and
// never saving history (observe and clean persist observations).
func (a *app) runReport(ctx context.Context, opts runOptions, summary bool) int {
	if summary && opts.json {
		return a.fatal(errors.New("--summary cannot be combined with --json"))
	}
	s, code := a.scan(ctx, opts)
	if code != 0 {
		return code
	}
	r := report.Report{
		Candidates: s.candidates,
		Skipped:    s.skipped,
		Warnings:   s.warnings,
		Now:        a.now(),
	}
	render := report.RenderHuman
	switch {
	case opts.json:
		render = report.RenderJSON
	case summary:
		render = report.RenderSummary
	}
	if err := render(a.stdout, r); err != nil {
		return a.fatal(err)
	}
	return exitCode(runOutcome{SkippedCollectors: len(s.skipped)})
}
