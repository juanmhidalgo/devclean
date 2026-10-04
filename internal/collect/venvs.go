package collect

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/history"
)

const venvsCategory = "venvs"

// Venvs collects central virtualenvs. The directories are injected: the
// caller derives them from the platform (pipenv: $WORKON_HOME or the data
// home's virtualenvs; poetry: the cache home's pypoetry/virtualenvs).
//
// A pipenv venv is garbage only when its .project pointer names a path that
// no longer exists. A missing, unreadable or empty pointer proves nothing, so
// the venv is manual. Poetry venvs have no proof rule and are always manual.
type Venvs struct {
	PipenvDir string
	PoetryDir string
}

var _ Collector = (*Venvs)(nil)

// Name returns the collector's category name.
func (v *Venvs) Name() string { return venvsCategory }

// Collect lists the venvs in each central directory and classifies them. A
// missing directory yields no candidates and is not an error.
func (v *Venvs) Collect(ctx context.Context) Result {
	res := Result{Revalidators: map[string]func(context.Context) classify.Decision{}}
	cov := history.Coverage{Category: venvsCategory, Complete: true, Seen: map[string]bool{}}
	for _, src := range []struct {
		dir      string
		classify func(venvDir string) classify.Decision
	}{
		{v.PipenvDir, ClassifyPipenvVenv},
		{v.PoetryDir, ClassifyPoetryVenv},
	} {
		if src.dir == "" {
			continue
		}
		cov.Roots = append(cov.Roots, src.dir)
		entries, err := os.ReadDir(src.dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if ctx.Err() != nil {
				cov.Complete = false
				break
			}
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(src.dir, e.Name())
			cov.Seen[venvsCategory+":"+path] = true
			d := src.classify(path)
			if d.Tier == classify.TierNone {
				continue
			}
			classifyVenv := src.classify
			res.Revalidators[path] = func(context.Context) classify.Decision { return classifyVenv(path) }
			res.Candidates = append(res.Candidates, classify.Candidate{
				Category:   classify.CategoryVenvs,
				Tier:       d.Tier,
				Path:       path,
				Size:       dirSize(path),
				Reason:     d.Reason,
				ReclaimCmd: manualReclaim(d.Tier, path),
			})
		}
	}
	res.Coverage = []history.Coverage{cov}
	return res
}

func manualReclaim(t classify.Tier, path string) string {
	if t == classify.TierManual {
		return "rm -rf " + path
	}
	return ""
}

// ClassifyPipenvVenv judges one pipenv venv from fresh facts (it reads the
// .project pointer and stats the project path). Reusable for revalidation.
func ClassifyPipenvVenv(venvDir string) classify.Decision {
	raw, err := os.ReadFile(filepath.Join(venvDir, ".project"))
	if err != nil {
		return classify.Decision{Tier: classify.TierManual, Reason: "project pointer missing or unreadable"}
	}
	project := strings.TrimSpace(string(raw))
	if project == "" {
		return classify.Decision{Tier: classify.TierManual, Reason: "project pointer is empty"}
	}
	if _, err := os.Stat(project); os.IsNotExist(err) {
		return classify.Decision{Tier: classify.TierGarbage, Reason: "project " + project + " no longer exists"}
	} else if err != nil {
		return classify.Decision{Tier: classify.TierManual, Reason: "cannot check whether the project exists"}
	}
	return classify.Decision{Tier: classify.TierNone, Reason: "project exists"}
}

// ClassifyPoetryVenv judges one poetry venv: always manual, no proof rule.
func ClassifyPoetryVenv(string) classify.Decision {
	return classify.Decision{Tier: classify.TierManual, Reason: "poetry venv: no proof the project is gone"}
}
