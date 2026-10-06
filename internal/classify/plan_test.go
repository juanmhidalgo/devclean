package classify

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func paths(cs []Candidate) []string {
	out := []string{}
	for _, c := range cs {
		out = append(out, c.Path)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDiskPercent(t *testing.T) {
	// 100 blocks total, 10 reserved, 80 used, 10 avail: df shows 80/90 = 89%,
	// while used/total would be 80%.
	got := DiskPercent(80, 10)
	if math.Abs(got-88.8889) > 0.001 {
		t.Errorf("DiskPercent(80,10) = %v, want ~88.8889 (df-like)", got)
	}
	if DiskPercent(0, 0) != 0 {
		t.Errorf("DiskPercent(0,0) must be 0")
	}
}

func TestPlanClean(t *testing.T) {
	garbage := Candidate{Tier: TierGarbage, Category: CategoryProjects, Path: "g"}
	cacheHot := Candidate{Tier: TierCaches, Category: CategoryCaches, Path: "ch", FSID: "hot"}
	cacheCold := Candidate{Tier: TierCaches, Category: CategoryCaches, Path: "cc", FSID: "cold"}
	cacheUnknown := Candidate{Tier: TierCaches, Category: CategoryCaches, Path: "cu", FSID: "nope"}
	stale1 := Candidate{Tier: TierStale, Path: "s1"}
	stale2 := Candidate{Tier: TierStale, Path: "s2"}
	manual := Candidate{Tier: TierManual, Path: "m", ReclaimCmd: "x"}
	stats := map[string]FSStat{
		"hot":  {Used: 90, Avail: 10},
		"cold": {Used: 50, Avail: 50},
	}
	all := []Candidate{garbage, cacheHot, cacheCold, cacheUnknown, stale1, manual, stale2}

	tests := []struct {
		name       string
		opts       PlanOptions
		want       []string
		wantNotice bool
	}{
		{"tty no selection", PlanOptions{TTY: true}, []string{"g", "ch"}, false},
		{"tty selection", PlanOptions{TTY: true, Selection: []int{2}}, []string{"g", "ch", "s2"}, false},
		{"tty selection out of range ignored", PlanOptions{TTY: true, Selection: []int{0, 3}}, []string{"g", "ch"}, false},
		{"yes tty", PlanOptions{TTY: true, Yes: true}, []string{"g", "ch", "s1", "s2"}, false},
		{"yes no tty", PlanOptions{Yes: true}, []string{"g", "ch", "s1", "s2"}, false},
		{"no tty no yes", PlanOptions{Selection: []int{1}}, []string{"g", "ch"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := PlanClean(all, stats, 90, tt.opts)
			if got := paths(res.Delete); !equalStrings(got, tt.want) {
				t.Errorf("Delete = %v, want %v", got, tt.want)
			}
			staleNotice := false
			for _, n := range res.Notices {
				staleNotice = staleNotice || strings.HasPrefix(n, "stale items skipped")
			}
			if staleNotice != tt.wantNotice {
				t.Errorf("Notices = %v, want stale notice %v", res.Notices, tt.wantNotice)
			}
		})
	}

	t.Run("kept caches get one notice per filesystem saying why", func(t *testing.T) {
		cacheCold2 := Candidate{Tier: TierCaches, Category: CategoryCaches, Path: "cc2", FSID: "cold"}
		res := PlanClean([]Candidate{cacheHot, cacheCold, cacheCold2, cacheUnknown}, stats, 85, PlanOptions{TTY: true})
		want := []string{
			"caches kept: their filesystem is at 50.0%, below the 85% pressure threshold",
			"caches kept: their filesystem's usage is unknown",
		}
		if !reflect.DeepEqual(res.Notices, want) {
			t.Errorf("Notices = %q, want %q", res.Notices, want)
		}
		if res := PlanClean([]Candidate{cacheHot}, stats, 85, PlanOptions{TTY: true}); len(res.Notices) != 0 {
			t.Errorf("caches under pressure got notices %q", res.Notices)
		}
	})

	t.Run("pressure boundary is inclusive", func(t *testing.T) {
		res := PlanClean([]Candidate{cacheCold}, stats, 50, PlanOptions{TTY: true})
		if len(res.Delete) != 1 {
			t.Errorf("cache at exactly pressure must be selected, got %v", paths(res.Delete))
		}
	})
}
