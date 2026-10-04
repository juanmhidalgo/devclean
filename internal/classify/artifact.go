package classify

import "time"

// ClassifyArtifact decides whether an artifact is stale. It is stale only
// inside a git work tree, when its newest use signal is older than threshold.
func ClassifyArtifact(facts ArtifactFacts, now time.Time, threshold time.Duration) Decision {
	if !facts.InGitWorkTree {
		return Decision{Tier: TierNone, Reason: "not inside a git work tree"}
	}

	var last LastUse
	consider := func(at time.Time, src Signal) {
		if !at.IsZero() && at.After(last.At) {
			last = LastUse{At: at, Source: src}
		}
	}
	consider(facts.CommitTime, SignalCommit)
	consider(facts.HeadMTime, SignalHead)
	consider(facts.IndexMTime, SignalIndex)
	consider(facts.ArtifactMTime, SignalArtifactMTime)
	if !facts.NoAtime {
		for _, at := range facts.MarkerATimes {
			consider(at, SignalMarkerATime)
		}
	}
	if last.Source == SignalNone {
		// History only adds evidence; alone it can never make an artifact a candidate.
		return Decision{Tier: TierNone, Reason: "no use signals available"}
	}
	consider(facts.HistoryLastUsed, SignalHistory)

	if now.Sub(last.At) > threshold {
		return Decision{Tier: TierStale, Reason: "last use older than threshold", LastUse: last}
	}
	return Decision{Tier: TierNone, Reason: "used within threshold", LastUse: last}
}
