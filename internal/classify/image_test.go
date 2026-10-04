package classify

import (
	"testing"
	"time"
)

func TestClassifyImage(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	threshold := 90 * 24 * time.Hour
	old := now.Add(-threshold - time.Hour)
	recent := now.Add(-time.Hour)
	eph := Ephemeral{Label: "devclean.ephemeral=true", Globs: []string{"ci-*", "tmp/*"}}

	tests := []struct {
		name       string
		facts      ImageFacts
		wantTier   Tier
		wantSource Signal
		wantAt     time.Time
	}{
		{"dangling no container is garbage", ImageFacts{Dangling: true}, TierGarbage, SignalNone, time.Time{}},
		{"dangling with container is none", ImageFacts{Dangling: true, HasContainer: true}, TierNone, SignalNone, time.Time{}},
		{"ephemeral label no container is garbage", ImageFacts{Tagged: true, Labels: map[string]string{"devclean.ephemeral": "true"}}, TierGarbage, SignalNone, time.Time{}},
		{"ephemeral label wrong value is not garbage", ImageFacts{Tagged: true, Labels: map[string]string{"devclean.ephemeral": "false"}, LastSeen: recent}, TierNone, SignalImageLastSeen, recent},
		{"ephemeral glob no container is garbage", ImageFacts{Tagged: true, Names: []string{"ci-build:latest"}}, TierGarbage, SignalNone, time.Time{}},
		// MUTATION: see docs/mutation-checks.md (one ephemeral tag among real ones).
		{"ephemeral glob needs every name to match", ImageFacts{Tagged: true, Names: []string{"ci-build:latest", "app:prod"}, LastSeen: recent}, TierNone, SignalImageLastSeen, recent},
		{"ephemeral glob with container is none", ImageFacts{Tagged: true, HasContainer: true, Names: []string{"ci-build:latest"}}, TierNone, SignalNone, time.Time{}},
		{"tagged old history is stale", ImageFacts{Tagged: true, Names: []string{"app:1"}, LastSeen: old, FirstSeen: recent}, TierStale, SignalImageLastSeen, old},
		{"tagged old first_seen without history is stale", ImageFacts{Tagged: true, Names: []string{"app:1"}, FirstSeen: old}, TierStale, SignalFirstSeen, old},
		{"tagged recent history is none", ImageFacts{Tagged: true, Names: []string{"app:1"}, LastSeen: recent, FirstSeen: old}, TierNone, SignalImageLastSeen, recent},
		{"exactly at threshold is not stale", ImageFacts{Tagged: true, Names: []string{"app:1"}, LastSeen: now.Add(-threshold)}, TierNone, SignalImageLastSeen, now.Add(-threshold)},
		{"old tagged with container is none", ImageFacts{Tagged: true, HasContainer: true, LastSeen: old}, TierNone, SignalNone, time.Time{}},
		{"no history and zero first_seen is none", ImageFacts{Tagged: true, Names: []string{"app:1"}}, TierNone, SignalNone, time.Time{}},
		{"untagged non-dangling old is none", ImageFacts{LastSeen: old}, TierNone, SignalNone, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyImage(tt.facts, now, threshold, eph)
			if got.Tier != tt.wantTier {
				t.Fatalf("tier = %v, want %v (reason %q)", got.Tier, tt.wantTier, got.Reason)
			}
			if got.LastUse.Source != tt.wantSource || !got.LastUse.At.Equal(tt.wantAt) {
				t.Errorf("last use = %+v, want {%v %v}", got.LastUse, tt.wantAt, tt.wantSource)
			}
			if got.Reason == "" {
				t.Error("reason is empty")
			}
		})
	}
}
