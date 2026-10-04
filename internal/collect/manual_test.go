package collect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/platform"
)

func TestSystemAndWatchCollectors(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) string {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	img1 := write("vm/a.qcow2", "12345")
	img2 := write("vm/b.qcow2", "123")
	write("vm/keep.txt", "x")
	write("old/data/f", "1234567")
	oldDir := filepath.Join(root, "old", "data")

	sys := func(items []platform.SystemItem, skips []string) *System {
		return &System{Items: func(context.Context) ([]platform.SystemItem, []string) { return items, skips }}
	}
	watch := func(paths ...string) *Watch {
		return &Watch{Config: config.Config{Watch: paths}, Home: root}
	}

	tests := []struct {
		name      string
		c         Collector
		wantName  string
		wantPaths []string
		wantCmds  []string
		wantSkip  string
	}{
		{"system items are manual with commands (AC-11)",
			sys([]platform.SystemItem{{Name: "core20 1891", Size: 9, Reason: "disabled snap revision", ReclaimCmd: "sudo snap remove core20 --revision=1891"}}, nil),
			"system", []string{"core20 1891"}, []string{"sudo snap remove core20 --revision=1891"}, ""},
		{"system skip is reported", sys(nil, []string{"snap: not installed"}),
			"system", nil, nil, "snap: not installed"},
		{"watch glob and dir (AC-11)", watch(filepath.Join(root, "vm", "*.qcow2"), oldDir, filepath.Join(root, "missing")),
			"watch", []string{img1, img2, oldDir},
			[]string{"rm " + img1, "rm " + img2, "rm -rf " + oldDir}, ""},
		{"watch expands tilde", watch("~/vm/a.qcow2"),
			"watch", []string{img1}, []string{"rm " + img1}, ""},
		{"empty watch list", watch(), "watch", nil, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.c.Name() != tt.wantName {
				t.Errorf("Name() = %q, want %q", tt.c.Name(), tt.wantName)
			}
			res := tt.c.Collect(context.Background())
			if len(res.Candidates) != len(tt.wantPaths) {
				t.Fatalf("candidates = %+v, want paths %v", res.Candidates, tt.wantPaths)
			}
			for i, c := range res.Candidates {
				if c.Tier != classify.TierManual {
					t.Errorf("%s tier = %v, want manual", c.Path, c.Tier)
				}
				if c.Path != tt.wantPaths[i] || c.ReclaimCmd != tt.wantCmds[i] {
					t.Errorf("candidate %d = %q / %q, want %q / %q", i, c.Path, c.ReclaimCmd, tt.wantPaths[i], tt.wantCmds[i])
				}
				if c.Category != classify.CategorySystem && c.Category != classify.CategoryWatch {
					t.Errorf("category = %v", c.Category)
				}
			}
			if tt.wantSkip == "" && len(res.Skipped) != 0 {
				t.Errorf("skipped = %+v", res.Skipped)
			}
			if tt.wantSkip != "" && (len(res.Skipped) != 1 ||
				res.Skipped[0].Collector != "system" || !strings.Contains(res.Skipped[0].Reason, tt.wantSkip)) {
				t.Errorf("skipped = %+v, want %q", res.Skipped, tt.wantSkip)
			}
		})
	}

	t.Run("watch sizes", func(t *testing.T) {
		res := watch(img1, oldDir).Collect(context.Background())
		if len(res.Candidates) != 2 || res.Candidates[0].Size != 5 || res.Candidates[1].Size != 7 {
			t.Errorf("candidates = %+v", res.Candidates)
		}
	})
}
