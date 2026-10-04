package classify

import (
	"testing"
	"time"
)

func TestClassifyArtifact(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	const threshold = 90 * 24 * time.Hour
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	day := 24 * time.Hour
	old := 200 * day

	oldFacts := func() ArtifactFacts {
		return ArtifactFacts{
			InGitWorkTree: true,
			CommitTime:    ago(old),
			HeadMTime:     ago(old),
			IndexMTime:    ago(old),
			ArtifactMTime: ago(old),
		}
	}
	with := func(mod func(*ArtifactFacts)) ArtifactFacts {
		f := oldFacts()
		mod(&f)
		return f
	}

	tests := []struct {
		name       string
		facts      ArtifactFacts
		wantTier   Tier
		wantSource Signal
		wantAt     time.Time
	}{
		{"AC-12 all signals old is stale", oldFacts(), TierStale, SignalCommit, ago(old)},
		{"AC-12 recent commit is not stale", with(func(f *ArtifactFacts) { f.CommitTime = ago(10 * day) }), TierNone, SignalCommit, ago(10 * day)},
		{"AC-12 recent HEAD mtime is not stale", with(func(f *ArtifactFacts) { f.HeadMTime = ago(10 * day) }), TierNone, SignalHead, ago(10 * day)},
		{"AC-12 recent index mtime is not stale", with(func(f *ArtifactFacts) { f.IndexMTime = ago(10 * day) }), TierNone, SignalIndex, ago(10 * day)},
		{"AC-12 recent artifact mtime is not stale", with(func(f *ArtifactFacts) { f.ArtifactMTime = ago(10 * day) }), TierNone, SignalArtifactMTime, ago(10 * day)},
		{"AC-12 recent marker atime is not stale", with(func(f *ArtifactFacts) { f.MarkerATimes = []time.Time{ago(150 * day), ago(10 * day)} }), TierNone, SignalMarkerATime, ago(10 * day)},
		{"AC-12 newest of several wins", with(func(f *ArtifactFacts) { f.CommitTime = ago(100 * day); f.HeadMTime = ago(20 * day) }), TierNone, SignalHead, ago(20 * day)},
		{"AC-12 missing signals are ignored", ArtifactFacts{InGitWorkTree: true, CommitTime: ago(old)}, TierStale, SignalCommit, ago(old)},
		{"AC-12 exactly at threshold is not stale", with(func(f *ArtifactFacts) { f.CommitTime = ago(threshold) }), TierNone, SignalCommit, ago(threshold)},
		{"AC-12 no signals at all is not stale", ArtifactFacts{InGitWorkTree: true}, TierNone, SignalNone, time.Time{}},

		// MUTATION: dropping the InGitWorkTree check (classifying stale
		// regardless of git membership) must make this row fail.
		{"AC-15 not in git work tree is never stale", with(func(f *ArtifactFacts) { f.InGitWorkTree = false }), TierNone, SignalNone, time.Time{}},

		{"pin NoAtime ignores recent marker atime", with(func(f *ArtifactFacts) { f.NoAtime = true; f.MarkerATimes = []time.Time{ago(1 * day)} }), TierStale, SignalCommit, ago(old)},
		{"pin NoAtime false honours marker atime", with(func(f *ArtifactFacts) { f.MarkerATimes = []time.Time{ago(1 * day)} }), TierNone, SignalMarkerATime, ago(1 * day)},

		// MUTATION (AC-22 add-only): letting history replace the other
		// signals (or lower last-use) instead of taking the max must make the
		// next two rows fail.
		{"AC-22 old history cannot make a recent artifact stale", with(func(f *ArtifactFacts) { f.CommitTime = ago(10 * day); f.HistoryLastUsed = ago(old) }), TierNone, SignalCommit, ago(10 * day)},
		{"AC-22 recent history moves toward not stale", with(func(f *ArtifactFacts) { f.HistoryLastUsed = ago(5 * day) }), TierNone, SignalHistory, ago(5 * day)},
		{"AC-22 old history on stale artifact stays stale from commit", with(func(f *ArtifactFacts) { f.HistoryLastUsed = ago(old + 50*day) }), TierStale, SignalCommit, ago(old)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyArtifact(tt.facts, now, threshold)
			if got.Tier != tt.wantTier {
				t.Errorf("tier = %v, want %v (reason %q)", got.Tier, tt.wantTier, got.Reason)
			}
			if got.Reason == "" {
				t.Errorf("reason is empty")
			}
			if got.LastUse.Source != tt.wantSource {
				t.Errorf("last-use source = %v, want %v", got.LastUse.Source, tt.wantSource)
			}
			if !got.LastUse.At.Equal(tt.wantAt) {
				t.Errorf("last-use at = %v, want %v", got.LastUse.At, tt.wantAt)
			}
		})
	}

	t.Run("pin zero history equals stateless decision", func(t *testing.T) {
		for _, tt := range tests {
			stateless := tt.facts
			stateless.HistoryLastUsed = time.Time{}
			if tt.facts.HistoryLastUsed.IsZero() {
				a := ClassifyArtifact(tt.facts, now, threshold)
				b := ClassifyArtifact(stateless, now, threshold)
				if a != b {
					t.Errorf("%s: zero history %+v != stateless %+v", tt.name, a, b)
				}
			}
		}
	})

	t.Run("pin history only moves toward not stale", func(t *testing.T) {
		// MUTATION: letting history alone decide staleness (AC-22) makes a non-stale artifact stale here.
		for _, tt := range tests {
			base := tt.facts
			base.HistoryLastUsed = time.Time{}
			without := ClassifyArtifact(base, now, threshold)
			for _, h := range []time.Time{ago(1 * day), ago(100 * day), ago(old * 2)} {
				withH := base
				withH.HistoryLastUsed = h
				got := ClassifyArtifact(withH, now, threshold)
				if got.Tier == TierStale && without.Tier != TierStale {
					t.Errorf("%s: history %v made a non-stale artifact stale", tt.name, h)
				}
				if got.LastUse.At.Before(without.LastUse.At) {
					t.Errorf("%s: history %v lowered last-use from %v to %v", tt.name, h, without.LastUse.At, got.LastUse.At)
				}
			}
		}
	})
}
