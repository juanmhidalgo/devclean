package remove

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/juanmhidalgo/devclean/internal/classify"
)

// Status is what the executor did with one candidate.
type Status int

const (
	StatusDeleted Status = iota
	StatusSkipped
	StatusFailed
)

// Outcome is the result for one candidate. Reason is set for skipped items,
// Err for failed ones.
type Outcome struct {
	Candidate classify.Candidate
	Status    Status
	Reason    string
	Err       error
}

// Executor deletes candidates, re-validating each one immediately before.
type Executor struct {
	// Revalidators maps Candidate.Path to the collector-supplied function that
	// re-runs the same Classify* rule on freshly gathered facts.
	Revalidators map[string]func(context.Context) classify.Decision
	DeleteTree   func(path string) error
	DeleteImage  func(ctx context.Context, id string, refs []string) error
	RunAction    func(ctx context.Context, cmd string) error
}

// NewExecutor returns an Executor wired to the real deleters.
func NewExecutor(revalidators map[string]func(context.Context) classify.Decision) *Executor {
	return &Executor{
		Revalidators: revalidators,
		DeleteTree:   RemoveTree,
		DeleteImage:  RemoveImage,
		RunAction: func(ctx context.Context, cmd string) error {
			argv := strings.Fields(cmd)
			if len(argv) == 0 {
				return fmt.Errorf("empty action command")
			}
			if out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %w: %s", cmd, err, strings.TrimSpace(string(out)))
			}
			return nil
		},
	}
}

// Run processes the candidates in order. A failure never stops the rest.
func (e *Executor) Run(ctx context.Context, cands []classify.Candidate) []Outcome {
	out := make([]Outcome, 0, len(cands))
	for _, c := range cands {
		out = append(out, e.one(ctx, c))
	}
	return out
}

func isAction(c classify.Candidate) bool {
	return c.Tier == classify.TierGarbage && c.ReclaimCmd != ""
}

func (e *Executor) one(ctx context.Context, c classify.Candidate) Outcome {
	skip := func(reason string) Outcome { return Outcome{Candidate: c, Status: StatusSkipped, Reason: reason} }
	if c.Tier == classify.TierManual {
		return skip("manual")
	}
	if reval := e.Revalidators[c.Path]; reval != nil {
		if d := reval(ctx); d.Tier != c.Tier {
			return skip(d.Reason)
		}
	} else if !isAction(c) {
		// Trivially "still garbage" actions need no revalidation; anything
		// else without a revalidator is not deleted.
		return skip("no revalidation")
	}
	var err error
	switch {
	case isAction(c):
		err = e.RunAction(ctx, c.ReclaimCmd)
	case c.Category == classify.CategoryDocker:
		err = e.DeleteImage(ctx, c.Path, c.Refs)
	default:
		err = e.DeleteTree(c.Path)
	}
	if err != nil {
		return Outcome{Candidate: c, Status: StatusFailed, Err: err}
	}
	return Outcome{Candidate: c, Status: StatusDeleted}
}
