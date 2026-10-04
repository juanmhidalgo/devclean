package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	home := "/home/tester"
	defaults := Default(home)

	t.Run("defaults", func(t *testing.T) {
		// AC-34
		if !reflect.DeepEqual(defaults.Scan.Roots, []string{home}) || !defaults.Scan.SkipHidden {
			t.Errorf("scan = %+v", defaults.Scan)
		}
		if defaults.ThresholdDays("projects") != 90 || defaults.PressurePercent != 85 {
			t.Errorf("thresholds/pressure = %v / %d", defaults.Thresholds, defaults.PressurePercent)
		}
		if defaults.Docker.EphemeralLabel != "devclean.ephemeral=true" || len(defaults.Watch) != 0 {
			t.Errorf("docker/watch = %+v / %v", defaults.Docker, defaults.Watch)
		}
		if defaults.ScanBurst.MinArtifacts != 5 || defaults.ScanBurst.Window.D() != 5*time.Minute {
			t.Errorf("burst = %+v", defaults.ScanBurst)
		}
		if defaults.ObserveInterval.D() != 24*time.Hour || defaults.CleanCadence != "weekly" {
			t.Errorf("intervals = %v / %q", defaults.ObserveInterval, defaults.CleanCadence)
		}
		names := map[string]string{}
		for _, a := range defaults.ArtifactTypes {
			names[a.Name+"/"+a.Marker] = a.MarkerLocation
		}
		for k, loc := range map[string]string{
			"node_modules/package.json": "beside", ".venv/pyvenv.cfg": "inside",
			"venv/pyvenv.cfg": "inside", ".tox/tox.ini": "beside",
			".tox/pyproject.toml": "beside", "target/Cargo.toml": "beside",
		} {
			if names[k] != loc {
				t.Errorf("default artifact type %s = %q, want %q", k, names[k], loc)
			}
		}
	})

	t.Run("missing file gives defaults", func(t *testing.T) {
		// AC-34; pin: no warning, no error
		got, err := Load(filepath.Join(t.TempDir(), "nope.toml"), home)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if !reflect.DeepEqual(got, defaults) {
			t.Errorf("got %+v, want defaults", got)
		}
	})

	t.Run("full file overlays defaults", func(t *testing.T) {
		// AC-35
		got, err := Load(filepath.Join("testdata", "full.toml"), home)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		want := Default(home)
		want.Scan.Roots = []string{"/work", "/play"}
		want.Scan.Excludes = []string{"/work/keep"}
		want.Thresholds["projects"] = 30
		want.Thresholds["docker"] = 14
		want.PressurePercent = 90
		want.ScanBurst = ScanBurst{MinArtifacts: 8, Window: Duration(10 * time.Minute)}
		want.Docker = Docker{EphemeralLabel: "my.label=yes", EphemeralGlobs: []string{"ci-*", "tmp/*"}}
		want.Watch = []string{"/vm/*.qcow2", "/vm/disk.img"}
		want.ArtifactTypes = append(want.ArtifactTypes, ArtifactType{Name: "build", Marker: "Makefile", MarkerLocation: "beside"})
		want.CachePaths = []CachePath{{Path: "/var/cache/thing", Tier: "caches"}}
		want.ObserveInterval = Duration(12 * time.Hour)
		want.CleanCadence = "daily"
		want.NotifyCommand = "my-notifier --flag"
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got  %+v\nwant %+v", got, want)
		}
	})

	t.Run("partial file keeps other defaults", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "c.toml")
		if err := os.WriteFile(p, []byte("pressure_percent = 70\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := Load(p, home)
		if err != nil {
			t.Fatal(err)
		}
		want := Default(home)
		want.PressurePercent = 70
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}

func TestLoadInvalid(t *testing.T) {
	// AC-36
	cases := []struct{ file, key string }{
		{"invalid_unknown_key.toml", "bogus_key"},
		{"invalid_wrong_type.toml", "pressure_percent"},
		{"invalid_no_marker.toml", "marker"},
		{"invalid_tier.toml", "tier"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			path := filepath.Join("testdata", c.file)
			_, err := Load(path, "/home/tester")
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), c.key) || !strings.Contains(err.Error(), path) {
				t.Errorf("error %q must name key %q and path %q", err, c.key, path)
			}
		})
	}
}
