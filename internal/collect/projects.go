package collect

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/platform"
)

const projectsCategory = "projects"

// Projects collects stale build artifacts (node_modules, venvs, ...) under
// the configured scan roots.
type Projects struct {
	Config  config.Config
	Mount   func(path string) (platform.Mount, error)
	History history.History
	Now     func() time.Time
}

var _ Collector = (*Projects)(nil)

// Name returns the collector's category name.
func (p *Projects) Name() string { return projectsCategory }

type projectArtifact struct {
	Artifact
	facts classify.ArtifactFacts
	mount platform.Mount
}

// Collect walks the roots, gathers facts, drops scan-burst atime reads, and
// classifies every artifact. Artifacts on a noatime mount produce one warning
// per mount and their atimes are neither used nor recorded.
func (p *Projects) Collect(ctx context.Context) Result {
	res := Result{Revalidators: map[string]func(context.Context) classify.Decision{}}
	walk := WalkArtifacts(p.Config.Scan.Roots, p.Config.ArtifactTypes, p.Config.Scan.Excludes)
	cov := history.Coverage{
		Category: projectsCategory, Roots: walk.Roots, Unreadable: walk.Unreadable,
		Complete: true, Seen: map[string]bool{},
	}

	warned := map[string]bool{}
	var arts []projectArtifact
	var reads []classify.Read
	for _, a := range walk.Artifacts {
		if ctx.Err() != nil {
			cov.Complete = false
			break
		}
		cov.Seen[p.key(a.Path)] = true
		pa := projectArtifact{Artifact: a, facts: GatherArtifactFacts(a)}
		m, err := p.Mount(a.Path)
		switch {
		case err != nil:
			res.Warnings = append(res.Warnings, fmt.Sprintf("cannot determine mount of %s: %v", a.Path, err))
		case m.NoAtime:
			pa.mount = m
			pa.facts.NoAtime = true
			if !warned[m.Point] {
				warned[m.Point] = true
				res.Warnings = append(res.Warnings, fmt.Sprintf("%s is mounted noatime: access times are unreliable, using git evidence only", m.Point))
			}
		default:
			pa.mount = m
			for _, at := range pa.facts.MarkerATimes {
				reads = append(reads, classify.Read{ArtifactID: a.Path, At: at})
			}
		}
		arts = append(arts, pa)
	}

	kept := classify.FilterScanBursts(reads, p.Config.ScanBurst.MinArtifacts, p.Config.ScanBurst.Window.D())
	surviving := map[string][]time.Time{}
	for _, r := range kept {
		surviving[r.ArtifactID] = append(surviving[r.ArtifactID], r.At)
		res.Observations = append(res.Observations, Observation{Key: p.key(r.ArtifactID), At: r.At})
	}

	now := p.Now()
	threshold := time.Duration(p.Config.ThresholdDays(projectsCategory)) * 24 * time.Hour
	for _, pa := range arts {
		pa.facts.MarkerATimes = surviving[pa.Path]
		if e, ok := p.History.Entries[p.key(pa.Path)]; ok {
			pa.facts.HistoryLastUsed = e.LastUsed
		}
		d := classify.ClassifyArtifact(pa.facts, now, threshold)
		if d.Tier != classify.TierStale {
			continue
		}
		res.Revalidators[pa.Path] = p.revalidator(pa, threshold)
		res.Candidates = append(res.Candidates, classify.Candidate{
			Category: classify.CategoryProjects,
			Tier:     d.Tier,
			Path:     pa.Path,
			Size:     dirSize(pa.Path),
			FSID:     pa.mount.Device,
			Reason:   d.Reason,
			LastUse:  d.LastUse,
		})
	}
	res.Coverage = []history.Coverage{cov}
	return res
}

// revalidator re-gathers the artifact's facts and re-runs ClassifyArtifact,
// with the history last-use as of the call.
func (p *Projects) revalidator(pa projectArtifact, threshold time.Duration) func(context.Context) classify.Decision {
	return func(context.Context) classify.Decision {
		facts := GatherArtifactFacts(pa.Artifact)
		facts.NoAtime = pa.facts.NoAtime
		if facts.NoAtime {
			facts.MarkerATimes = nil
		}
		if e, ok := p.History.Entries[p.key(pa.Path)]; ok {
			facts.HistoryLastUsed = e.LastUsed
		}
		return classify.ClassifyArtifact(facts, p.Now(), threshold)
	}
}

func (p *Projects) key(path string) string { return projectsCategory + ":" + path }

// dirSize sums the sizes of regular files under dir without following symlinks.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += fi.Size()
		}
		return nil
	})
	return total
}
