package remove

import (
	"reflect"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/platform"
)

func TestMeasureFreed(t *testing.T) {
	use := func(avail uint64) platform.FSUsage { return platform.FSUsage{Avail: avail} }
	tests := []struct {
		name          string
		before, after map[string]platform.FSUsage
		want          map[string]int64
	}{
		{"freed bytes (AC-30)",
			map[string]platform.FSUsage{"8:1": use(1000)},
			map[string]platform.FSUsage{"8:1": use(1600)},
			map[string]int64{"8:1": 600}},
		{"statfs delta, not item sum, when layers are shared (AC-30)",
			// items summing to 900 shared 400 bytes; the delta is what was freed
			map[string]platform.FSUsage{"8:1": use(1000)},
			map[string]platform.FSUsage{"8:1": use(1500)},
			map[string]int64{"8:1": 500}},
		{"avail can drop when something else wrote",
			map[string]platform.FSUsage{"8:1": use(1000)},
			map[string]platform.FSUsage{"8:1": use(900)},
			map[string]int64{"8:1": -100}},
		{"one entry per filesystem",
			map[string]platform.FSUsage{"8:1": use(10), "8:2": use(100)},
			map[string]platform.FSUsage{"8:1": use(30), "8:2": use(100)},
			map[string]int64{"8:1": 20, "8:2": 0}},
		{"filesystem on one side only is omitted",
			map[string]platform.FSUsage{"8:1": use(10), "8:2": use(5)},
			map[string]platform.FSUsage{"8:1": use(30), "8:3": use(5)},
			map[string]int64{"8:1": 20}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MeasureFreed(tt.before, tt.after); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MeasureFreed = %v, want %v", got, tt.want)
			}
		})
	}
}
