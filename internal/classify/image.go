package classify

import (
	"path"
	"strings"
	"time"
)

// ImageFacts are the pre-gathered signals for one Docker image. CreatedAt and
// LastTagTime are deliberately not inputs. Zero times mean "signal missing".
type ImageFacts struct {
	Dangling     bool
	Tagged       bool
	HasContainer bool // any container, running or stopped
	Labels       map[string]string
	Names        []string // repo:tag references
	LastSeen     time.Time
	FirstSeen    time.Time
}

// Ephemeral says which images are disposable by label or name glob.
type Ephemeral struct {
	Label string // "key=value"
	Globs []string
}

// ClassifyImage decides whether an image is garbage, stale, or neither.
func ClassifyImage(facts ImageFacts, now time.Time, threshold time.Duration, ephemeral Ephemeral) Decision {
	if facts.HasContainer {
		return Decision{Tier: TierNone, Reason: "a container depends on the image"}
	}
	if facts.Dangling {
		return Decision{Tier: TierGarbage, Reason: "dangling image with no container"}
	}
	if ephemeral.matches(facts) {
		return Decision{Tier: TierGarbage, Reason: "ephemeral image with no container"}
	}
	if !facts.Tagged {
		return Decision{Tier: TierNone, Reason: "image is not tagged"}
	}

	last := LastUse{At: facts.LastSeen, Source: SignalImageLastSeen}
	if last.At.IsZero() {
		last = LastUse{At: facts.FirstSeen, Source: SignalFirstSeen}
	}
	if last.At.IsZero() {
		return Decision{Tier: TierNone, Reason: "no use evidence available"}
	}
	if now.Sub(last.At) > threshold {
		return Decision{Tier: TierStale, Reason: "last use older than threshold", LastUse: last}
	}
	return Decision{Tier: TierNone, Reason: "used within threshold", LastUse: last}
}

func (e Ephemeral) matches(facts ImageFacts) bool {
	if k, v, ok := strings.Cut(e.Label, "="); ok && k != "" {
		if got, present := facts.Labels[k]; present && got == v {
			return true
		}
	}
	// By name, every tag must be ephemeral: the image is removed by all of its
	// refs, so one scratch tag must not take a real tag down with it.
	if len(facts.Names) == 0 {
		return false
	}
	for _, name := range facts.Names {
		if !e.nameMatches(name) {
			return false
		}
	}
	return true
}

func (e Ephemeral) nameMatches(name string) bool {
	for _, glob := range e.Globs {
		if ok, _ := path.Match(glob, name); ok {
			return true
		}
	}
	return false
}
