package collect

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/classify"
)

func TestVenvsCollector(t *testing.T) {
	str := func(s string) *string { return &s }

	cases := []struct {
		name     string
		poetry   bool
		project  func(live string) *string
		skipRoot bool
		wantTier classify.Tier
		wantCand bool
	}{
		{name: "AC-26 pipenv project gone is garbage", project: func(string) *string { return str("/nonexistent/gone/project\n") }, wantCand: true, wantTier: classify.TierGarbage},
		{name: "AC-26 pipenv project exists is not a candidate", project: func(live string) *string { return &live }},
		// MUTATION: treat a missing .project as "project gone" -> the venv becomes garbage
		{name: "AC-26 pipenv missing .project is manual", project: func(string) *string { return nil }, wantCand: true, wantTier: classify.TierManual},
		{name: "AC-26 pipenv empty .project is manual", project: func(string) *string { return str("  \n") }, wantCand: true, wantTier: classify.TierManual},
		{name: "AC-26 pipenv unreadable .project is manual", project: func(string) *string { return str("/nonexistent/gone") }, skipRoot: true, wantCand: true, wantTier: classify.TierManual},
		{name: "AC-26 poetry venv is manual even when project gone", poetry: true, project: func(string) *string { return str("/nonexistent/gone") }, wantCand: true, wantTier: classify.TierManual},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipRoot && os.Geteuid() == 0 {
				t.Skip("root reads mode-000 files")
			}
			root := t.TempDir()
			live := filepath.Join(root, "live")
			if err := os.Mkdir(live, 0o755); err != nil {
				t.Fatal(err)
			}
			central := filepath.Join(root, "central")
			vdir := filepath.Join(central, "proj-AbC123")
			if err := os.MkdirAll(vdir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(vdir, "pyvenv.cfg"), []byte("12345"), 0o644); err != nil {
				t.Fatal(err)
			}
			if p := tc.project(live); p != nil {
				pf := filepath.Join(vdir, ".project")
				if err := os.WriteFile(pf, []byte(*p), 0o644); err != nil {
					t.Fatal(err)
				}
				if tc.skipRoot {
					if err := os.Chmod(pf, 0); err != nil {
						t.Fatal(err)
					}
				}
			}
			v := &Venvs{}
			if tc.poetry {
				v.PoetryDir = central
			} else {
				v.PipenvDir = central
			}
			res := v.Collect(context.Background())

			if len(res.Skipped) != 0 || len(res.Warnings) != 0 {
				t.Fatalf("unexpected skipped/warnings: %+v %+v", res.Skipped, res.Warnings)
			}
			if len(res.Coverage) != 1 || res.Coverage[0].Category != "venvs" || !res.Coverage[0].Complete || !res.Coverage[0].Seen["venvs:"+vdir] {
				t.Fatalf("coverage = %+v", res.Coverage)
			}
			if !tc.wantCand {
				if len(res.Candidates) != 0 {
					t.Fatalf("want no candidates, got %+v", res.Candidates)
				}
				return
			}
			if len(res.Candidates) != 1 {
				t.Fatalf("want 1 candidate, got %+v", res.Candidates)
			}
			c := res.Candidates[0]
			if c.Category != classify.CategoryVenvs || c.Path != vdir || c.Tier != tc.wantTier || c.Size < 5 || c.Reason == "" {
				t.Fatalf("candidate = %+v", c)
			}
			if tc.wantTier == classify.TierManual && c.ReclaimCmd != "rm -rf "+vdir {
				t.Fatalf("ReclaimCmd = %q", c.ReclaimCmd)
			}
		})
	}
}

func TestVenvsCollectorMissingDirs(t *testing.T) {
	v := &Venvs{PipenvDir: "/nonexistent/a", PoetryDir: "/nonexistent/b"}
	res := v.Collect(context.Background())
	if len(res.Candidates) != 0 || len(res.Skipped) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestVenvsRevalidator(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	vdir := filepath.Join(root, "central", "p-1")
	if err := os.MkdirAll(vdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, ".project"), []byte(proj), 0o644); err != nil {
		t.Fatal(err)
	}
	res := (&Venvs{PipenvDir: filepath.Join(root, "central")}).Collect(context.Background())
	reval := res.Revalidators[vdir]
	if reval == nil {
		t.Fatal("no revalidator")
	}
	if d := reval(context.Background()); d.Tier != classify.TierGarbage {
		t.Fatalf("project gone: tier = %v", d.Tier)
	}
	if err := os.Mkdir(proj, 0o755); err != nil { // project dir reappeared
		t.Fatal(err)
	}
	if d := reval(context.Background()); d.Tier != classify.TierNone {
		t.Fatalf("project reappeared: tier = %v", d.Tier)
	}
}
