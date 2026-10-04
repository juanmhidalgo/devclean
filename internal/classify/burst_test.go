package classify

import (
	"fmt"
	"testing"
	"time"
)

func TestFilterScanBursts(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(id string, min float64) Read {
		return Read{ArtifactID: id, At: base.Add(time.Duration(min * float64(time.Minute)))}
	}
	burst := func(n int, startMin float64) []Read {
		var rs []Read
		for i := 0; i < n; i++ {
			rs = append(rs, at(fmt.Sprintf("b%d", i), startMin+float64(i)*0.5))
		}
		return rs
	}
	cat := func(parts ...[]Read) []Read {
		var out []Read
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}

	tests := []struct {
		name   string
		reads  []Read
		min    int
		window time.Duration
		want   int
	}{
		{"AC-14 five distinct in window dropped", burst(5, 0), 5, 5 * time.Minute, 0},
		{"AC-14 four distinct kept", burst(4, 0), 5, 5 * time.Minute, 4},
		{"AC-14 five reads of one artifact kept", []Read{at("a", 0), at("a", 1), at("a", 2), at("a", 3), at("a", 4)}, 5, 5 * time.Minute, 5},
		{"AC-14 five spread beyond window kept", []Read{at("a", 0), at("b", 3), at("c", 6), at("d", 9), at("e", 12)}, 5, 5 * time.Minute, 5},
		{"AC-14 isolated read survives next to burst", cat(burst(5, 0), []Read{at("lone", 30)}), 5, 5 * time.Minute, 1},
		{"AC-14 span exactly equal to window counts as shared", []Read{at("a", 0), at("b", 1), at("c", 2), at("d", 3), at("e", 5)}, 5, 5 * time.Minute, 0},
		{"min is a parameter", burst(3, 0), 3, 5 * time.Minute, 0},
		{"window is a parameter", []Read{at("a", 0), at("b", 3), at("c", 6), at("d", 9), at("e", 12)}, 5, 15 * time.Minute, 0},
		{"empty", nil, 5, 5 * time.Minute, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FilterScanBursts(tc.reads, tc.min, tc.window)
			if len(got) != tc.want {
				t.Fatalf("got %d reads, want %d", len(got), tc.want)
			}
		})
	}

	t.Run("reads tied with an earlier read share its window", func(t *testing.T) {
		// Sorted by time, "a"@0 comes after its ties; the window opening at 0
		// must still count all five.
		rs := []Read{at("b", 0), at("c", 0), at("d", 0), at("e", 0), at("a", 0)}
		if got := FilterScanBursts(rs, 5, 5*time.Minute); len(got) != 0 {
			t.Fatalf("got %d reads, want 0", len(got))
		}
	})

	t.Run("matches the quadratic definition on mixed input", func(t *testing.T) {
		var rs []Read
		for i := 0; i < 400; i++ {
			// Uneven spacing and repeated IDs so windows overlap partially.
			rs = append(rs, at(fmt.Sprintf("x%d", i%7), float64((i*37)%400)*0.6))
		}
		got := FilterScanBursts(rs, 4, 2*time.Minute)
		want := naiveFilterScanBursts(rs, 4, 2*time.Minute)
		if len(want) == 0 || len(want) == len(rs) {
			t.Fatalf("fixture must both drop and keep reads, kept %d of %d", len(want), len(rs))
		}
		if len(got) != len(want) {
			t.Fatalf("got %d reads, want %d", len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("read %d = %+v, want %+v", i, got[i], want[i])
			}
		}
	})

	t.Run("scales to one read per package across many node_modules", func(t *testing.T) {
		// 100k reads took tens of seconds with the quadratic scan.
		var rs []Read
		for i := 0; i < 100_000; i++ {
			rs = append(rs, at(fmt.Sprintf("nm%d", i%200), float64(i)/1000))
		}
		start := time.Now()
		FilterScanBursts(rs, 5, 5*time.Minute)
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("took %v for 100k reads", d)
		}
	})

	t.Run("kept reads are the isolated ones", func(t *testing.T) {
		got := FilterScanBursts(cat(burst(5, 0), []Read{at("lone", 30)}), 5, 5*time.Minute)
		if len(got) != 1 || got[0].ArtifactID != "lone" {
			t.Fatalf("got %+v, want only lone", got)
		}
	})
}

// naiveFilterScanBursts is the quadratic reference definition: every read
// opens a window, and a window with enough distinct artifacts drops all of
// its reads.
func naiveFilterScanBursts(reads []Read, minArtifacts int, window time.Duration) []Read {
	drop := make([]bool, len(reads))
	for _, start := range reads {
		end := start.At.Add(window)
		distinct := map[string]struct{}{}
		var inWindow []int
		for j, r := range reads {
			if !r.At.Before(start.At) && !r.At.After(end) {
				distinct[r.ArtifactID] = struct{}{}
				inWindow = append(inWindow, j)
			}
		}
		if len(distinct) >= minArtifacts {
			for _, j := range inWindow {
				drop[j] = true
			}
		}
	}
	var kept []Read
	for i, r := range reads {
		if !drop[i] {
			kept = append(kept, r)
		}
	}
	return kept
}
