package remove

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/classify"
)

type calls struct {
	trees, images, actions []string
	refs                   [][]string
}

func newExec(c *calls, reval map[string]func(context.Context) classify.Decision) *Executor {
	return &Executor{
		Revalidators: reval,
		DeleteTree:   func(p string) error { c.trees = append(c.trees, p); return nil },
		DeleteImage: func(_ context.Context, id string, refs []string) error {
			c.images = append(c.images, id)
			c.refs = append(c.refs, refs)
			return nil
		},
		RunAction: func(_ context.Context, cmd string) error { c.actions = append(c.actions, cmd); return nil },
	}
}

func still(t classify.Tier) func(context.Context) classify.Decision {
	return func(context.Context) classify.Decision { return classify.Decision{Tier: t, Reason: "still"} }
}

func TestExecutorRevalidates(t *testing.T) {
	stale := func(p string) classify.Candidate {
		return classify.Candidate{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: p}
	}
	tests := []struct {
		name       string
		cand       classify.Candidate
		reval      func(context.Context) classify.Decision
		wantStatus Status
		wantReason string
		wantTrees  []string
		wantImages []string
		wantAct    []string
	}{
		{name: "still qualifies is deleted", cand: stale("/a"), reval: still(classify.TierStale),
			wantStatus: StatusDeleted, wantTrees: []string{"/a"}},
		// MUTATION: revalidate replaced by a location check (os.Stat of Path)
		// would delete this: the path is unchanged but it was used since the report (AC-25).
		{name: "used since report is skipped though path exists", cand: stale(os.TempDir()),
			reval: func(context.Context) classify.Decision {
				return classify.Decision{Tier: classify.TierNone, Reason: "used since the report"}
			},
			wantStatus: StatusSkipped, wantReason: "used since the report"},
		{name: "tier changed is skipped", cand: stale("/c"), reval: still(classify.TierManual),
			wantStatus: StatusSkipped, wantReason: "still"},
		{name: "nil revalidate is skipped", cand: stale("/d"), reval: nil,
			wantStatus: StatusSkipped, wantReason: "no revalidation"},
		{name: "image no longer qualifies", cand: classify.Candidate{Category: classify.CategoryDocker, Tier: classify.TierStale, Path: "sha256:x"},
			reval:      func(context.Context) classify.Decision { return classify.Decision{Reason: "container uses it"} },
			wantStatus: StatusSkipped, wantReason: "container uses it"},
		{name: "image qualifies is removed", cand: classify.Candidate{Category: classify.CategoryDocker, Tier: classify.TierStale, Path: "sha256:y"},
			reval: still(classify.TierStale), wantStatus: StatusDeleted, wantImages: []string{"sha256:y"}},
		{name: "garbage action without revalidate runs", cand: classify.Candidate{Category: classify.CategoryCaches, Tier: classify.TierGarbage, Path: "uv cache prune", ReclaimCmd: "uv cache prune"},
			wantStatus: StatusDeleted, wantAct: []string{"uv cache prune"}},
		{name: "manual refused", cand: classify.Candidate{Tier: classify.TierManual, Path: "/m"}, reval: still(classify.TierManual),
			wantStatus: StatusSkipped, wantReason: "manual"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var c calls
			reval := map[string]func(context.Context) classify.Decision{}
			if tc.reval != nil {
				reval[tc.cand.Path] = tc.reval
			}
			out := newExec(&c, reval).Run(context.Background(), []classify.Candidate{tc.cand})
			if len(out) != 1 || out[0].Status != tc.wantStatus || out[0].Reason != tc.wantReason {
				t.Fatalf("outcome = %+v, want status %v reason %q", out, tc.wantStatus, tc.wantReason)
			}
			if !reflect.DeepEqual(c.trees, tc.wantTrees) || !reflect.DeepEqual(c.images, tc.wantImages) || !reflect.DeepEqual(c.actions, tc.wantAct) {
				t.Fatalf("calls = %+v", c)
			}
		})
	}
}

// Pin: a failed deletion is recorded and the remaining items still run.
func TestExecutorFailureDoesNotStop(t *testing.T) {
	var c calls
	e := newExec(&c, map[string]func(context.Context) classify.Decision{
		"/1": still(classify.TierStale), "/2": still(classify.TierStale),
	})
	boom := errors.New("boom")
	e.DeleteTree = func(p string) error {
		c.trees = append(c.trees, p)
		if p == "/1" {
			return boom
		}
		return nil
	}
	mk := func(p string) classify.Candidate {
		return classify.Candidate{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: p}
	}
	out := e.Run(context.Background(), []classify.Candidate{mk("/1"), mk("/2")})
	if out[0].Status != StatusFailed || !errors.Is(out[0].Err, boom) || out[1].Status != StatusDeleted {
		t.Fatalf("outcomes = %+v", out)
	}
	if !reflect.DeepEqual(c.trees, []string{"/1", "/2"}) {
		t.Fatalf("trees = %v", c.trees)
	}
}

// The default deleters really remove a tree.
func TestNewExecutorDeletesTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cand := classify.Candidate{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: dir}
	e := NewExecutor(map[string]func(context.Context) classify.Decision{dir: still(classify.TierStale)})
	out := e.Run(context.Background(), []classify.Candidate{cand})
	if out[0].Status != StatusDeleted {
		t.Fatalf("outcome = %+v", out)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir still exists: %v", err)
	}
}

func TestExecutorPassesImageRefs(t *testing.T) {
	var c calls
	cand := classify.Candidate{Category: classify.CategoryDocker, Tier: classify.TierGarbage, Path: "sha256:m", Refs: []string{"a:1", "b:2"}}
	x := newExec(&c, map[string]func(context.Context) classify.Decision{"sha256:m": still(classify.TierGarbage)})
	if out := x.Run(context.Background(), []classify.Candidate{cand}); out[0].Status != StatusDeleted {
		t.Fatalf("outcome = %+v", out[0])
	}
	if want := [][]string{{"a:1", "b:2"}}; !reflect.DeepEqual(c.refs, want) {
		t.Errorf("refs = %v, want %v", c.refs, want)
	}
}
