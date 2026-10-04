package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/platform"
)

func TestScheduleCommand(t *testing.T) {
	setup := func(t *testing.T, plat fakePlatform) (*reportEnv, *[]platform.ScheduleSpec, *int) {
		var installs []platform.ScheduleSpec
		var uninstalls int
		plat.installs, plat.uninstalls = &installs, &uninstalls
		e := newReportEnv(t, plat)
		e.a.executable = func() (string, error) { return "/opt/devclean", nil }
		t.Setenv("PATH", "/usr/bin:/bin")
		return e, &installs, &uninstalls
	}

	t.Run("install passes binary, PATH and default cadence (AC-37)", func(t *testing.T) {
		e, installs, _ := setup(t, fakePlatform{euid: 1000})
		if code := e.run("schedule", "install"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		want := []platform.ScheduleSpec{{Binary: "/opt/devclean", PATH: "/usr/bin:/bin", CleanCadence: "weekly"}}
		if !reflect.DeepEqual(*installs, want) {
			t.Errorf("installs = %+v, want %+v", *installs, want)
		}
	})

	t.Run("cadence comes from config and --report-only is passed on (AC-37)", func(t *testing.T) {
		e, installs, _ := setup(t, fakePlatform{euid: 1000})
		if err := os.MkdirAll(e.cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.cfgDir, "config.toml"), []byte("clean_cadence = \"daily\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if code := e.run("schedule", "install", "--report-only"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		want := []platform.ScheduleSpec{{Binary: "/opt/devclean", PATH: "/usr/bin:/bin", CleanCadence: "daily", ReportOnly: true}}
		if !reflect.DeepEqual(*installs, want) {
			t.Errorf("installs = %+v, want %+v", *installs, want)
		}
	})

	t.Run("invalid config exits 1 without installing", func(t *testing.T) {
		e, installs, _ := setup(t, fakePlatform{euid: 1000})
		if err := os.MkdirAll(e.cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(e.cfgDir, "config.toml"), []byte("not = [valid"), 0o644); err != nil {
			t.Fatal(err)
		}
		if code := e.run("schedule", "install"); code != 1 || len(*installs) != 0 {
			t.Errorf("exit = %d, installs = %d", code, len(*installs))
		}
	})

	t.Run("uninstall calls the platform", func(t *testing.T) {
		e, _, uninstalls := setup(t, fakePlatform{euid: 1000})
		if code := e.run("schedule", "uninstall"); code != 0 || *uninstalls != 1 {
			t.Errorf("exit = %d, uninstalls = %d", code, *uninstalls)
		}
	})

	t.Run("preflight gates both subcommands", func(t *testing.T) {
		e, installs, uninstalls := setup(t, fakePlatform{euid: 0})
		for _, sub := range []string{"install", "uninstall"} {
			if code := e.run("schedule", sub); code != 1 {
				t.Errorf("%s exit = %d, want 1", sub, code)
			}
		}
		if len(*installs) != 0 || *uninstalls != 0 {
			t.Errorf("platform reached despite preflight")
		}
	})

	t.Run("platform error exits 1 with the message", func(t *testing.T) {
		e, _, _ := setup(t, fakePlatform{euid: 1000, scheduleErr: errors.New("boom")})
		if code := e.run("schedule", "install"); code != 1 || !strings.Contains(e.stderr.String(), "boom") {
			t.Errorf("exit = %d, stderr %q", code, e.stderr.String())
		}
	})
}
