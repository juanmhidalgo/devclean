// Package collect gathers cleanup candidates from the machine.
package collect

import (
	"context"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/history"
)

// Result is what one collector found in one run.
type Result struct {
	Candidates   []classify.Candidate
	Coverage     []history.Coverage
	Skipped      []Skip
	Warnings     []string
	Observations []Observation
	// Revalidators maps Candidate.Path to a function that re-runs the same
	// Classify* rule on freshly gathered facts, called right before deletion.
	// A candidate without an entry has no revalidation.
	Revalidators map[string]func(context.Context) classify.Decision
}

// Skip records a collector that could not run, and why.
type Skip struct {
	Collector string
	Reason    string
}

// Observation is one fact to be recorded in history. Normally it is a
// trustworthy use of Key at At (merged into last_used). With FirstSeenOnly the
// item was merely seen at At: record first_seen if the key is new, and leave
// last_used alone.
type Observation struct {
	Key           string
	At            time.Time
	FirstSeenOnly bool
}

// Collector gathers candidates for one category.
type Collector interface {
	Name() string
	Collect(ctx context.Context) Result
}
