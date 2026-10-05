//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/testenv"
)

var unitNames = []string{
	"devclean-observe.service", "devclean-observe.timer",
	"devclean-clean.service", "devclean-clean.timer",
}

func readUnit(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("unit %s: %v", name, err)
	}
	return string(b)
}

func fakeSystemctl(t *testing.T) (log string) {
	t.Helper()
	log = filepath.Join(t.TempDir(), "calls")
	testenv.FakeBin(t, "systemctl", `echo "$@" >> '`+log+`'`)
	return log
}

func readCalls(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestLinuxScheduleUnits(t *testing.T) {
	spec := ScheduleSpec{Binary: "/opt/bin/devclean", PATH: "/usr/bin:/home/u/go/bin", CleanCadence: "daily"}

	t.Run("install writes units and enables timers (AC-37)", func(t *testing.T) {
		log := fakeSystemctl(t)
		dir := filepath.Join(t.TempDir(), "units")
		if err := (Linux{UnitDir: dir}).InstallSchedule(spec); err != nil {
			t.Fatal(err)
		}
		obs := readUnit(t, dir, "devclean-observe.service")
		for _, want := range []string{"Type=oneshot", "ExecStart=/opt/bin/devclean observe", "Environment=PATH=/usr/bin:/home/u/go/bin"} {
			if !strings.Contains(obs, want) {
				t.Errorf("observe service missing %q:\n%s", want, obs)
			}
		}
		clean := readUnit(t, dir, "devclean-clean.service")
		for _, want := range []string{"ExecStart=/opt/bin/devclean clean --quiet\n", "Environment=PATH=/usr/bin:/home/u/go/bin"} {
			if !strings.Contains(clean, want) {
				t.Errorf("clean service missing %q:\n%s", want, clean)
			}
		}
		if strings.Contains(clean, "--yes") {
			t.Errorf("unattended clean must not pass --yes:\n%s", clean)
		}
		ot := readUnit(t, dir, "devclean-observe.timer")
		for _, want := range []string{"OnCalendar=hourly", "Persistent=true", "WantedBy=timers.target"} {
			if !strings.Contains(ot, want) {
				t.Errorf("observe timer missing %q:\n%s", want, ot)
			}
		}
		ct := readUnit(t, dir, "devclean-clean.timer")
		if !strings.Contains(ct, "OnCalendar=daily") || !strings.Contains(ct, "Persistent=true") {
			t.Errorf("clean timer wrong:\n%s", ct)
		}
		want := []string{
			"--user daemon-reload",
			"--user enable --now devclean-observe.timer",
			"--user enable --now devclean-clean.timer",
		}
		got := readCalls(t, log)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("systemctl calls = %q, want %q", got, want)
		}
	})

	t.Run("report-only runs report (AC-37)", func(t *testing.T) {
		fakeSystemctl(t)
		dir := t.TempDir()
		s := spec
		s.ReportOnly = true
		if err := (Linux{UnitDir: dir}).InstallSchedule(s); err != nil {
			t.Fatal(err)
		}
		clean := readUnit(t, dir, "devclean-clean.service")
		if !strings.Contains(clean, "ExecStart=/opt/bin/devclean report\n") || strings.Contains(clean, "clean --quiet") {
			t.Errorf("clean service should run report:\n%s", clean)
		}
	})

	t.Run("uninstall disables and removes (AC-37)", func(t *testing.T) {
		log := fakeSystemctl(t)
		dir := t.TempDir()
		l := Linux{UnitDir: dir}
		if err := l.InstallSchedule(spec); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(log); err != nil {
			t.Fatal(err)
		}
		if err := l.UninstallSchedule(); err != nil {
			t.Fatal(err)
		}
		for _, n := range unitNames {
			if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
				t.Errorf("%s still present (err=%v)", n, err)
			}
		}
		got := strings.Join(readCalls(t, log), "|")
		for _, want := range []string{"--user disable --now devclean-observe.timer", "--user disable --now devclean-clean.timer"} {
			if !strings.Contains(got, want) {
				t.Errorf("calls %q missing %q", got, want)
			}
		}
	})

	t.Run("systemctl failure surfaces output", func(t *testing.T) {
		testenv.FakeBin(t, "systemctl", `echo boom; exit 3`)
		err := (Linux{UnitDir: t.TempDir()}).InstallSchedule(spec)
		if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "boom") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("binary path and PATH with spaces and % stay literal", func(t *testing.T) {
		fakeSystemctl(t)
		dir := t.TempDir()
		s := spec
		s.Binary = "/home/u/my tools/100%/dev$clean"
		s.PATH = "/usr/bin:/mnt/c/Program Files/x%y"
		if err := (Linux{UnitDir: dir}).InstallSchedule(s); err != nil {
			t.Fatal(err)
		}
		clean := readUnit(t, dir, "devclean-clean.service")
		for _, want := range []string{
			`ExecStart="/home/u/my tools/100%%/dev$clean" clean --quiet` + "\n",
			`Environment="PATH=/usr/bin:/mnt/c/Program Files/x%%y"` + "\n",
		} {
			if !strings.Contains(clean, want) {
				t.Errorf("clean service missing %q:\n%s", want, clean)
			}
		}
	})

	t.Run("invalid cadence is rejected before any unit is written", func(t *testing.T) {
		log := fakeSystemctl(t)
		testenv.FakeBin(t, "systemd-analyze", `echo "Failed to parse calendar specification '$2'" >&2; exit 1`)
		dir := filepath.Join(t.TempDir(), "units")
		for _, cadence := range []string{"garbage", "daily\nExecStart=/bin/evil"} {
			s := spec
			s.CleanCadence = cadence
			err := (Linux{UnitDir: dir}).InstallSchedule(s)
			if err == nil || !strings.Contains(err.Error(), "clean_cadence") {
				t.Errorf("%q: err = %v", cadence, err)
			}
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("unit dir created for an invalid cadence (err=%v)", err)
		}
		if _, err := os.Stat(log); !os.IsNotExist(err) {
			t.Errorf("systemctl was called for an invalid cadence")
		}
	})

	t.Run("failed enable rolls back the partial install", func(t *testing.T) {
		log := filepath.Join(t.TempDir(), "calls")
		testenv.FakeBin(t, "systemctl", `echo "$@" >> '`+log+`'; case "$*" in *"enable --now devclean-clean.timer") echo nope; exit 1;; esac`)
		dir := t.TempDir()
		err := (Linux{UnitDir: dir}).InstallSchedule(spec)
		if err == nil || !strings.Contains(err.Error(), "nope") {
			t.Fatalf("err = %v", err)
		}
		for _, n := range unitNames {
			if _, err := os.Stat(filepath.Join(dir, n)); !os.IsNotExist(err) {
				t.Errorf("%s left behind (err=%v)", n, err)
			}
		}
		if got := strings.Join(readCalls(t, log), "|"); !strings.Contains(got, "--user disable --now devclean-observe.timer") {
			t.Errorf("observe timer not disabled on rollback: %q", got)
		}
	})

	t.Run("default unit dir is under the config home", func(t *testing.T) {
		_, cfg := testenv.Isolate(t)
		want := filepath.Join(cfg, "systemd", "user")
		if got := (Linux{}).unitDir(); got != want {
			t.Errorf("unitDir = %q, want %q", got, want)
		}
	})
}
