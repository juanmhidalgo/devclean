package collect

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/testenv"
)

var dockerNow = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

// fakeDockerScript dispatches on "$1 $2" like the real CLI.
const fakeDockerScript = `
case "$1 $2" in
"image ls") cat <<'EOF'
{"ID":"sha256:aaa","Repository":"<none>","Tag":"<none>","Size":"100MB"}
{"ID":"sha256:bbb","Repository":"tmp","Tag":"1","Size":"1.5GB"}
{"ID":"sha256:ccc","Repository":"scratch-x","Tag":"1","Size":"20kB"}
{"ID":"sha256:ddd","Repository":"old","Tag":"1","Size":"2MB"}
{"ID":"sha256:eee","Repository":"fresh","Tag":"1","Size":"3MB"}
{"ID":"sha256:fff","Repository":"web","Tag":"1","Size":"4MB"}
{"ID":"sha256:ggg","Repository":"<none>","Tag":"<none>","Size":"5MB"}
EOF
;;
"image inspect") case "$5" in
  sha256:bbb) echo '{"devclean.ephemeral":"true"}';;
  *) echo null;;
esac;;
"ps -a") cat <<'EOF'
{"ID":"c1","Image":"web:1","State":"running"}
{"ID":"c2","Image":"ggg","State":"exited"}
EOF
;;
"volume ls") cat <<'EOF'
{"Name":"vol1","Driver":"local"}
{"Name":"vol2","Driver":"local"}
EOF
;;
"system df") cat <<'EOF'
{"Type":"Images","Reclaimable":"1GB (10%)"}
{"Type":"Build Cache","Reclaimable":"2.5GB (100%)"}
EOF
;;
*) echo "unexpected: $*" >&2; exit 2;;
esac
`

func newDocker(h history.History) *Docker {
	cfg := config.Default("/home/x")
	cfg.Docker.EphemeralGlobs = []string{"scratch-*"}
	return &Docker{Config: cfg, History: h, Now: func() time.Time { return dockerNow }}
}

func TestDockerCollector(t *testing.T) {
	t.Run("classifies images, cache and volumes", func(t *testing.T) {
		testenv.FakeBin(t, "docker", fakeDockerScript)
		h := history.History{Entries: map[string]history.Entry{
			"docker:sha256:ddd": {FirstSeen: dockerNow.AddDate(0, 0, -200), LastUsed: dockerNow.AddDate(0, 0, -100)},
		}}
		res := newDocker(h).Collect(context.Background())

		if len(res.Skipped) != 0 {
			t.Fatalf("unexpected skip: %+v", res.Skipped)
		}
		got := map[string]classify.Candidate{}
		for _, c := range res.Candidates {
			got[c.Path] = c
		}
		want := map[string]classify.Tier{
			"sha256:aaa":           classify.TierGarbage,
			"sha256:bbb":           classify.TierGarbage,
			"sha256:ccc":           classify.TierGarbage,
			"sha256:ddd":           classify.TierStale,
			"docker builder prune": classify.TierGarbage,
			"vol1":                 classify.TierManual,
			"vol2":                 classify.TierManual,
		}
		if len(got) != len(want) {
			t.Errorf("candidates = %v, want paths %v", got, want)
		}
		for path, tier := range want {
			c, ok := got[path]
			if !ok || c.Tier != tier || c.Category != classify.CategoryDocker {
				t.Errorf("%s: got %+v, want tier %v", path, c, tier)
			}
		}
		if c := got["vol1"]; c.ReclaimCmd != "docker volume rm vol1" {
			t.Errorf("vol1 ReclaimCmd = %q", c.ReclaimCmd)
		}
		if c := got["docker builder prune"]; c.ReclaimCmd != "docker builder prune -f" || c.Size != 2_500_000_000 {
			t.Errorf("builder prune = %+v", c)
		}
		if c := got["sha256:bbb"]; c.Size != 1_500_000_000 {
			t.Errorf("bbb size = %d", c.Size)
		}
		if c := got["sha256:ddd"]; c.LastUse.Source != classify.SignalImageLastSeen {
			t.Errorf("ddd last use = %+v", c.LastUse)
		}

		obs := map[string]Observation{}
		for _, o := range res.Observations {
			obs[o.Key] = o
		}
		for _, id := range []string{"sha256:fff", "sha256:ggg"} { // containers
			if o, ok := obs["docker:"+id]; !ok || o.FirstSeenOnly || !o.At.Equal(dockerNow) {
				t.Errorf("%s: observation = %+v, want in-use at now", id, o)
			}
		}
		if o, ok := obs["docker:sha256:eee"]; !ok || !o.FirstSeenOnly {
			t.Errorf("eee: observation = %+v, want first-seen-only", o)
		}
		if _, ok := obs["docker:sha256:ddd"]; ok {
			t.Errorf("ddd already has history; no observation expected")
		}
		if len(res.Coverage) != 1 || !res.Coverage[0].Complete || len(res.Coverage[0].Seen) != 7 ||
			!res.Coverage[0].Seen["docker:sha256:fff"] {
			t.Errorf("coverage = %+v", res.Coverage)
		}
	})

	t.Run("new tagged image is not stale", func(t *testing.T) {
		testenv.FakeBin(t, "docker", fakeDockerScript)
		res := newDocker(history.History{}).Collect(context.Background())
		for _, c := range res.Candidates {
			if c.Tier == classify.TierStale {
				t.Errorf("stale candidate without history: %+v", c)
			}
		}
	})

	t.Run("skips when docker cannot run", func(t *testing.T) {
		cases := []struct {
			name   string
			setup  func(t *testing.T)
			reason string
		}{
			{"daemon unreachable", func(t *testing.T) {
				testenv.FakeBin(t, "docker", `echo "Cannot connect to the Docker daemon at unix:///var/run/docker.sock" >&2; exit 1`)
			}, "Cannot connect to the Docker daemon"},
			{"no binary", func(t *testing.T) { t.Setenv("PATH", t.TempDir()) }, "not found"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tc.setup(t)
				res := newDocker(history.History{}).Collect(context.Background())
				if len(res.Skipped) != 1 || res.Skipped[0].Collector != "docker" ||
					!strings.Contains(res.Skipped[0].Reason, tc.reason) {
					t.Fatalf("skipped = %+v, want reason containing %q", res.Skipped, tc.reason)
				}
				if len(res.Candidates) != 0 || len(res.Coverage) != 1 || res.Coverage[0].Complete {
					t.Errorf("candidates=%v coverage=%+v", res.Candidates, res.Coverage)
				}
			})
		}
	})
}

func TestDockerImageRevalidator(t *testing.T) {
	flag := t.TempDir() + "/has-container"
	script := strings.Replace(fakeDockerScript, `"ps -a") cat <<'EOF'`,
		`"ps -a") if [ -f `+flag+` ]; then echo '{"ID":"c9","Image":"aaa","State":"running"}'; fi; cat <<'EOF'`, 1)
	script = strings.Replace(script, `"docker failing"`, "", 1)
	testenv.FakeBin(t, "docker", script)
	res := newDocker(history.History{}).Collect(context.Background())
	reval := res.Revalidators["sha256:aaa"]
	if reval == nil {
		t.Fatal("no revalidator for the dangling image")
	}
	if d := reval(context.Background()); d.Tier != classify.TierGarbage {
		t.Fatalf("unchanged: tier = %v (%s)", d.Tier, d.Reason)
	}
	if err := os.WriteFile(flag, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if d := reval(context.Background()); d.Tier != classify.TierNone {
		t.Fatalf("container now uses it: tier = %v (%s)", d.Tier, d.Reason)
	}
}

func TestDockerImageRevalidatorDockerFails(t *testing.T) {
	flag := t.TempDir() + "/broken"
	script := strings.Replace(fakeDockerScript, `"ps -a") cat <<'EOF'`,
		`"ps -a") if [ -f `+flag+` ]; then echo daemon down >&2; exit 1; fi; cat <<'EOF'`, 1)
	testenv.FakeBin(t, "docker", script)
	res := newDocker(history.History{}).Collect(context.Background())
	reval := res.Revalidators["sha256:aaa"]
	if err := os.WriteFile(flag, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if d := reval(context.Background()); d.Tier != classify.TierNone || !strings.Contains(d.Reason, "daemon down") {
		t.Fatalf("docker failure: %+v", d)
	}
}

// docker image ls prints one row per tag; the rows of one ID are one candidate.
func TestDockerCollectorGroupsTagsByID(t *testing.T) {
	// MUTATION: see docs/mutation-checks.md (one candidate per tag row).
	script := strings.Replace(fakeDockerScript, `{"ID":"sha256:ggg"`,
		`{"ID":"sha256:mmm","Repository":"scratch-a","Tag":"1","Size":"7MB"}
{"ID":"sha256:mmm","Repository":"scratch-b","Tag":"2","Size":"7MB"}
{"ID":"sha256:nnn","Repository":"scratch-c","Tag":"1","Size":"8MB"}
{"ID":"sha256:nnn","Repository":"keep","Tag":"1","Size":"8MB"}
{"ID":"sha256:ggg"`, 1)
	testenv.FakeBin(t, "docker", script)
	res := newDocker(history.History{}).Collect(context.Background())
	var mmm []classify.Candidate
	for _, c := range res.Candidates {
		switch c.Path {
		case "sha256:mmm":
			mmm = append(mmm, c)
		case "sha256:nnn":
			t.Errorf("image with a non-ephemeral tag became a candidate: %+v", c)
		}
	}
	if len(mmm) != 1 {
		t.Fatalf("sha256:mmm candidates = %d, want 1: %+v", len(mmm), mmm)
	}
	if c := mmm[0]; c.Tier != classify.TierGarbage || c.Size != 7_000_000 ||
		strings.Join(c.Refs, " ") != "scratch-a:1 scratch-b:2" {
		t.Errorf("candidate = %+v", c)
	}
	if res.Revalidators["sha256:mmm"] == nil {
		t.Error("no revalidator for the grouped image")
	}
}
