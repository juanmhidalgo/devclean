package main

import (
	"errors"
	"testing"
)

func TestExitCode(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		o    runOutcome
		want int
	}{
		{"clean", runOutcome{}, 0},
		{"above pressure only", runOutcome{AbovePressure: true}, 0},
		{"fatal", runOutcome{Fatal: boom}, 1},
		{"fatal wins over skipped", runOutcome{Fatal: boom, SkippedCollectors: 1}, 1},
		{"fatal wins over everything", runOutcome{Fatal: boom, SkippedCollectors: 1, FailedDeletions: 2, NotifyErr: boom}, 1},
		{"skipped collector", runOutcome{SkippedCollectors: 1}, 2},
		{"failed deletion", runOutcome{FailedDeletions: 1}, 2},
		{"notify failed", runOutcome{NotifyErr: boom}, 2},
		{"partial plus pressure", runOutcome{SkippedCollectors: 1, AbovePressure: true}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.o); got != tt.want {
				t.Fatalf("exitCode(%+v) = %d, want %d", tt.o, got, tt.want)
			}
		})
	}
}

func TestRunOutcomeSummary(t *testing.T) {
	o := runOutcome{Fatal: errors.New("x"), SkippedCollectors: 2, FailedDeletions: 3, AbovePressure: true}
	s := o.summary()
	if !s.Fatal || !s.AbovePressure || s.SkippedCollectors != 2 || s.FailedDeletions != 3 {
		t.Fatalf("summary mismatch: %+v", s)
	}
	if (runOutcome{}).summary().Fatal {
		t.Fatal("nil Fatal must not set Summary.Fatal")
	}
}
