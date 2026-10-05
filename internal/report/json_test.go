package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/remove"
)

func renderJSON(t *testing.T, r Report) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := RenderJSON(&buf, r); err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	if !json.Valid(buf.Bytes()) {
		t.Fatalf("invalid JSON: %s", buf.String())
	}
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if dec.More() {
		t.Fatalf("trailing data after document")
	}
	if strings.TrimRight(buf.String(), "\n") == buf.String() {
		t.Fatalf("expected trailing newline")
	}
	return doc
}

func jsonFixture() Report {
	last := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	return Report{
		Now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC),
		Candidates: []classify.Candidate{
			{Category: classify.CategoryProjects, Tier: classify.TierStale, Path: "/p/a/node_modules", Size: 300, Reason: "unused", LastUse: classify.LastUse{At: last, Source: classify.SignalArtifactMTime}},
			{Category: classify.CategoryDocker, Tier: classify.TierStale, Path: "sha256:abcdef", Size: 50, Reason: "old image", LastUse: classify.LastUse{At: last, Source: classify.SignalImageLastSeen}},
			{Category: classify.CategorySystem, Tier: classify.TierManual, Path: "snap old revisions", Size: 70, Reason: "manual", ReclaimCmd: "sudo snap remove x"},
			{Category: classify.CategoryDocker, Tier: classify.TierGarbage, Path: "docker builder prune", Size: 9, Reason: "build cache", ReclaimCmd: "docker builder prune -f"},
			{Category: classify.CategoryDocker, Tier: classify.TierManual, Path: "pgdata", Reason: "volumes may hold data", ReclaimCmd: "docker volume rm pgdata", SizeUnknown: true, UsedBy: []string{"db-1"}, Tip: "keep it small"},
		},
		Skipped:  []collect.Skip{{Collector: "docker", Reason: "daemon unreachable"}},
		Warnings: []string{"w1"},
	}
}

func TestRenderJSON(t *testing.T) {
	t.Run("report", func(t *testing.T) {
		doc := renderJSON(t, jsonFixture())
		if doc["schema_version"] != float64(1) {
			t.Fatalf("schema_version = %v", doc["schema_version"])
		}
		if _, ok := doc["freed_bytes_by_filesystem"]; ok {
			t.Fatal("report must not carry freed_bytes_by_filesystem")
		}
		cands := doc["candidates"].([]any)
		if len(cands) != 5 {
			t.Fatalf("candidates = %d", len(cands))
		}
		c0 := cands[0].(map[string]any)
		want := map[string]any{
			"category": "projects", "tier": "stale", "path": "/p/a/node_modules",
			"size_bytes": float64(300), "last_used": "2026-03-15T10:00:00Z",
			"last_used_source": "artifact_mtime", "reason": "unused", "index": float64(1),
		}
		for k, v := range want {
			if c0[k] != v {
				t.Errorf("c0[%s] = %v, want %v", k, c0[k], v)
			}
		}
		for _, k := range []string{"outcome", "command", "image_id"} {
			if _, ok := c0[k]; ok {
				t.Errorf("c0 has unexpected %s", k)
			}
		}
		c1 := cands[1].(map[string]any)
		if c1["image_id"] != "sha256:abcdef" || c1["index"] != float64(2) {
			t.Errorf("image candidate = %v", c1)
		}
		if _, ok := c1["path"]; ok {
			t.Error("image candidate must not have path")
		}
		c2 := cands[2].(map[string]any)
		if c2["command"] != "sudo snap remove x" || c2["last_used"] != nil || c2["last_used_source"] != nil {
			t.Errorf("manual candidate = %v", c2)
		}
		if _, ok := c2["index"]; ok {
			t.Error("non-stale has index")
		}
		c3 := cands[3].(map[string]any)
		if c3["action"] != "docker builder prune" || c3["command"] != "docker builder prune -f" {
			t.Errorf("action candidate = %v", c3)
		}
		c4 := cands[4].(map[string]any)
		if c4["volume"] != "pgdata" || c4["command"] != "docker volume rm pgdata" {
			t.Errorf("volume candidate = %v", c4)
		}
		if _, ok := c4["action"]; ok {
			t.Error("volume candidate must not have action")
		}
		if u, _ := c4["used_by"].([]any); len(u) != 1 || u[0] != "db-1" {
			t.Errorf("used_by = %v", c4["used_by"])
		}
		if c4["tip"] != "keep it small" {
			t.Errorf("tip = %v", c4["tip"])
		}
		if _, ok := c3["tip"]; ok {
			t.Errorf("candidate without a tip has tip: %v", c3)
		}
		if _, ok := c3["used_by"]; ok {
			t.Errorf("non-volume has used_by: %v", c3)
		}
		if c4["size_unknown"] != true {
			t.Errorf("unmeasured candidate lacks size_unknown: %v", c4)
		}
		if _, ok := c3["size_unknown"]; ok {
			t.Errorf("measured candidate has size_unknown: %v", c3)
		}
		sk := doc["skipped"].([]any)[0].(map[string]any)
		if sk["collector"] != "docker" || sk["reason"] != "daemon unreachable" {
			t.Errorf("skipped = %v", sk)
		}
		if w := doc["warnings"].([]any); len(w) != 1 || w[0] != "w1" {
			t.Errorf("warnings = %v", w)
		}
	})

	t.Run("empty slices are arrays", func(t *testing.T) {
		doc := renderJSON(t, Report{})
		for _, k := range []string{"candidates", "skipped", "warnings"} {
			if a, ok := doc[k].([]any); !ok || len(a) != 0 {
				t.Errorf("%s = %v", k, doc[k])
			}
		}
	})

	t.Run("clean", func(t *testing.T) {
		r := jsonFixture()
		cs := r.Candidates
		r.Outcomes = []remove.Outcome{
			{Candidate: cs[0], Status: remove.StatusDeleted},
			{Candidate: cs[1], Status: remove.StatusSkipped, Reason: "used since scan"},
			{Candidate: cs[3], Status: remove.StatusFailed, Err: errors.New("boom")},
		}
		r.Freed = map[string]int64{"fs1": 300}
		doc := renderJSON(t, r)
		if f := doc["freed_bytes_by_filesystem"].(map[string]any); f["fs1"] != float64(300) {
			t.Errorf("freed = %v", f)
		}
		cands := doc["candidates"].([]any)
		check := func(i int, outcome, reason any) {
			c := cands[i].(map[string]any)
			if c["outcome"] != outcome || c["outcome_reason"] != reason {
				t.Errorf("cand %d outcome=%v reason=%v", i, c["outcome"], c["outcome_reason"])
			}
		}
		check(0, "deleted", nil)
		check(1, "skipped", "used since scan")
		check(3, "failed", "boom")
		if _, ok := cands[2].(map[string]any)["outcome"]; ok {
			t.Error("candidate without outcome should omit it")
		}
	})

	t.Run("clean with nothing freed has empty map", func(t *testing.T) {
		doc := renderJSON(t, Report{Freed: map[string]int64{}})
		if f, ok := doc["freed_bytes_by_filesystem"].(map[string]any); !ok || len(f) != 0 {
			t.Errorf("freed = %v", doc["freed_bytes_by_filesystem"])
		}
	})
}
