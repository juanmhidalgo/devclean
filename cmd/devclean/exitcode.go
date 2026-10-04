package main

import "github.com/juanmhidalgo/devclean/internal/notify"

// runOutcome collects everything that decides the process exit code.
type runOutcome struct {
	Fatal             error
	SkippedCollectors int
	FailedDeletions   int
	NotifyErr         error
	AbovePressure     bool
}

// exitCode owns the 0/1/2 contract: 1 for a fatal error, 2 for a partial run
// (skipped collector, failed deletion or failed notification), 0 otherwise.
// Being above the pressure threshold never changes the result.
func exitCode(o runOutcome) int {
	switch {
	case o.Fatal != nil:
		return 1
	case o.SkippedCollectors > 0 || o.FailedDeletions > 0 || o.NotifyErr != nil:
		return 2
	default:
		return 0
	}
}

// summary derives the notification summary from the same outcome.
func (o runOutcome) summary() notify.Summary {
	return notify.Summary{
		AbovePressure:     o.AbovePressure,
		FailedDeletions:   o.FailedDeletions,
		SkippedCollectors: o.SkippedCollectors,
		Fatal:             o.Fatal != nil,
	}
}
