package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
)

func TestObserveCommand(t *testing.T) {
	e := newReportEnv(t, fakePlatform{euid: 1000})
	clock := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
	e.a.now = func() time.Time { return clock }
	at := clock.Add(-time.Hour)
	e.a.newCollectors = func(config.Config, history.History) []collect.Collector {
		return []collect.Collector{
			spyCollector{name: "projects", log: &e.calls, res: collect.Result{Observations: []collect.Observation{{Key: "projects:/p", At: at}}}},
			spyCollector{name: "docker", log: &e.calls, res: collect.Result{Observations: []collect.Observation{{Key: "docker:img", At: at}}}},
			spyCollector{name: "caches", log: &e.calls},
		}
	}
	load := func() history.History {
		h, _, err := history.Load(filepath.Join(e.a.platform.StateDir(), "history.json"), e.a.now)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	if code := e.run("observe"); code != 0 {
		t.Fatalf("first exit = %d, stderr %q", code, e.stderr.String())
	}
	if want := []string{"projects", "docker"}; !reflect.DeepEqual(e.calls, want) {
		t.Errorf("first run calls = %v, want %v", e.calls, want)
	}
	h := load()
	if !h.Entries["projects:/p"].LastUsed.Equal(at) || !h.Entries["docker:img"].LastUsed.Equal(at) {
		t.Errorf("observations not saved: %+v", h.Entries)
	}
	walked := clock
	if !h.Walked["projects"].Equal(walked) {
		t.Errorf("walked = %v, want %v", h.Walked["projects"], walked)
	}

	clock = clock.Add(time.Hour)
	if code := e.run("observe"); code != 0 {
		t.Fatalf("second exit = %d", code)
	}
	if want := []string{"docker"}; !reflect.DeepEqual(e.calls, want) {
		t.Errorf("second run calls = %v, want %v", e.calls, want)
	}
	h = load()
	if _, ok := h.Entries["projects:/p"]; !ok {
		t.Errorf("docker-only run pruned projects entry: %+v", h.Entries)
	}
	if !h.Walked["projects"].Equal(walked) {
		t.Errorf("walked moved on a docker-only run: %v", h.Walked["projects"])
	}

	clock = clock.Add(24 * time.Hour)
	e.run("observe")
	if want := []string{"projects", "docker"}; !reflect.DeepEqual(e.calls, want) {
		t.Errorf("run after interval calls = %v, want %v", e.calls, want)
	}
	if !load().Walked["projects"].Equal(clock) {
		t.Errorf("walked not refreshed")
	}
	// a skipped walk is not a walk: it retries next run and exits 2.
	skipped := true
	base := e.a.newCollectors
	e.a.newCollectors = func(c config.Config, h history.History) []collect.Collector {
		out := base(c, h)
		if skipped {
			out[0] = spyCollector{name: "projects", log: &e.calls, res: collect.Result{Skipped: []collect.Skip{{Collector: "projects", Reason: "x"}}}}
		}
		return out
	}
	clock = clock.Add(25 * time.Hour)
	if code := e.run("observe"); code != 2 {
		t.Errorf("skipped exit = %d, want 2", code)
	}
	if load().Walked["projects"].Equal(clock) {
		t.Errorf("skipped walk recorded as walked")
	}
	if len(e.deletes) != 0 {
		t.Errorf("observe deleted: %v", e.deletes)
	}

	// a --root walk covers part of the roots: it is not the daily walk.
	skipped = false
	before := load().Walked["projects"]
	clock = clock.Add(25 * time.Hour)
	if code := e.run("observe", "--root", t.TempDir()); code != 0 {
		t.Fatalf("--root exit = %d", code)
	}
	if want := []string{"projects", "docker"}; !reflect.DeepEqual(e.calls, want) {
		t.Errorf("--root run calls = %v, want %v", e.calls, want)
	}
	if got := load().Walked["projects"]; !got.Equal(before) {
		t.Errorf("--root walk recorded as the daily walk: %v, want %v", got, before)
	}
}
