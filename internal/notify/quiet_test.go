package notify

import "testing"

func TestShouldNotifyQuiet(t *testing.T) {
	tests := []struct {
		name  string
		s     Summary
		quiet bool
		want  bool
	}{
		{"loud all-zero", Summary{}, false, true},
		{"loud with trigger", Summary{Fatal: true}, false, true},
		{"quiet all-zero", Summary{}, true, false},
		{"quiet above pressure", Summary{AbovePressure: true}, true, true},
		{"quiet failed deletion", Summary{FailedDeletions: 1}, true, true},
		{"quiet skipped collector", Summary{SkippedCollectors: 1}, true, true},
		{"quiet fatal", Summary{Fatal: true}, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldNotify(tc.s, tc.quiet); got != tc.want {
				t.Errorf("ShouldNotify(%+v, %v) = %v, want %v", tc.s, tc.quiet, got, tc.want)
			}
		})
	}
}
