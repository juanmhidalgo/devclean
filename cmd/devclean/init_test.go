package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/platform"
	"github.com/juanmhidalgo/devclean/internal/testenv"
)

func TestInitCommand(t *testing.T) {
	// initEnv is a home with repos under ~/Develop (2) and ~/src (1) and a
	// 1 GiB sparse disk image, answering prompts from input.
	initEnv := func(t *testing.T, input string) (e *reportEnv, home string, installs *[]platform.ScheduleSpec) {
		t.Helper()
		var inst []platform.ScheduleSpec
		e = newReportEnv(t, fakePlatform{euid: 1000, installs: &inst})
		e.a.executable = func() (string, error) { return "/opt/devclean", nil }
		home = os.Getenv("HOME")
		for _, p := range []string{"Develop/a/.git", "Develop/b/.git", "src/c/.git"} {
			mkdirAll(t, filepath.Join(home, p))
		}
		img := filepath.Join(home, "vms", "kali", "kali.qcow2")
		mkdirAll(t, filepath.Dir(img))
		f, err := os.Create(img)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(imageMinSize); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		if input != "" {
			e.a.stdin = strings.NewReader(input)
			e.a.isTTY = func() bool { return true }
		}
		return e, home, &inst
	}
	cfgPath := func(e *reportEnv) string { return filepath.Join(e.cfgDir, "config.toml") }
	loadCfg := func(t *testing.T, e *reportEnv, home string) config.Config {
		t.Helper()
		cfg, err := config.Load(cfgPath(e), home)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}

	t.Run("interactive: chosen roots, watched images, tested notifier, trial schedule", func(t *testing.T) {
		body := filepath.Join(t.TempDir(), "body")
		testenv.FakeBin(t, "savenotify", "cat > "+body)
		// roots: only 1 · watch: default all · command · send test · arrived ·
		// schedule · no first observe
		e, home, installs := initEnv(t, "1\n\nsavenotify\n\n\n\nn\n")
		if code := e.run("init"); code != 0 {
			t.Fatalf("exit = %d, stderr %q\nstdout %s", code, e.stderr.String(), e.stdout.String())
		}
		cfg := loadCfg(t, e, home)
		if want := []string{filepath.Join(home, "Develop")}; !reflect.DeepEqual(cfg.Scan.Roots, want) {
			t.Errorf("roots = %v, want %v", cfg.Scan.Roots, want)
		}
		if want := []string{"~/vms/kali/*.qcow2"}; !reflect.DeepEqual(cfg.Watch, want) {
			t.Errorf("watch = %v, want %v", cfg.Watch, want)
		}
		if cfg.NotifyCommand != "savenotify" {
			t.Errorf("notify_command = %q", cfg.NotifyCommand)
		}
		if b, err := os.ReadFile(body); err != nil || !strings.Contains(string(b), "test notification") {
			t.Errorf("test notification body = %q (err %v)", b, err)
		}
		want := []platform.ScheduleSpec{{Binary: "/opt/devclean", PATH: os.Getenv("PATH"), CleanCadence: "weekly", ReportOnly: true, Notify: true}}
		if !reflect.DeepEqual(*installs, want) {
			t.Errorf("installs = %+v, want %+v", *installs, want)
		}
		if _, err := os.Stat(filepath.Join(e.a.platform.StateDir(), "history.json")); err == nil {
			t.Error("observe ran although declined")
		}
	})

	t.Run("a failed or undelivered test notification asks for the command again", func(t *testing.T) {
		testenv.FakeBin(t, "failnotify", "echo refused; exit 1")
		testenv.FakeBin(t, "silentnotify", "cat > /dev/null")
		testenv.FakeBin(t, "goodnotify", "cat > /dev/null")
		// roots all · watch all · failnotify, test (fails) · silentnotify,
		// test, not arrived · goodnotify, test, arrived · no schedule · no observe
		e, home, installs := initEnv(t, "\n\nfailnotify\n\nsilentnotify\n\nn\ngoodnotify\n\n\nn\nn\n")
		if code := e.run("init"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		if got := loadCfg(t, e, home).NotifyCommand; got != "goodnotify" {
			t.Errorf("notify_command = %q, want goodnotify", got)
		}
		if !strings.Contains(e.stderr.String(), "refused") {
			t.Errorf("stderr lacks the failure: %q", e.stderr.String())
		}
		if len(*installs) != 0 {
			t.Errorf("schedule installed although declined: %+v", *installs)
		}
	})

	t.Run("--yes takes everything found, no notifier, schedules and observes", func(t *testing.T) {
		e, home, installs := initEnv(t, "")
		if code := e.run("init", "--yes"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		cfg := loadCfg(t, e, home)
		if want := []string{filepath.Join(home, "Develop"), filepath.Join(home, "src")}; !reflect.DeepEqual(cfg.Scan.Roots, want) {
			t.Errorf("roots = %v, want %v", cfg.Scan.Roots, want)
		}
		if cfg.NotifyCommand != "" {
			t.Errorf("notify_command = %q, want empty", cfg.NotifyCommand)
		}
		if len(*installs) != 1 || !(*installs)[0].ReportOnly || (*installs)[0].Notify {
			t.Errorf("installs = %+v, want one report-only without notify", *installs)
		}
		if _, err := os.Stat(filepath.Join(e.a.platform.StateDir(), "history.json")); err != nil {
			t.Errorf("first observe did not run: %v", err)
		}
	})

	t.Run("--notify-command is written with --yes", func(t *testing.T) {
		e, home, installs := initEnv(t, "")
		if code := e.run("init", "--yes", "--notify-command", `notify -t "dev clean"`); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		if got := loadCfg(t, e, home).NotifyCommand; got != `notify -t "dev clean"` {
			t.Errorf("notify_command = %q", got)
		}
		if len(*installs) != 1 || !(*installs)[0].Notify {
			t.Errorf("installs = %+v, want report-only with notify", *installs)
		}
	})

	t.Run("an existing config is kept unless --force, which backs it up", func(t *testing.T) {
		e, home, installs := initEnv(t, "")
		old := "pressure_percent = 70\n"
		writeTestFile(t, cfgPath(e), old)
		if code := e.run("init", "--yes"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if !strings.Contains(e.stderr.String(), "--force") || len(*installs) != 0 {
			t.Errorf("stderr %q installs %+v", e.stderr.String(), *installs)
		}
		if b, _ := os.ReadFile(cfgPath(e)); string(b) != old {
			t.Errorf("config changed without --force: %q", b)
		}
		if code := e.run("init", "--yes", "--force"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		if b, _ := os.ReadFile(cfgPath(e) + ".bak"); string(b) != old {
			t.Errorf("backup = %q, want the old config", b)
		}
		if loadCfg(t, e, home).PressurePercent != 85 {
			t.Error("the new config was not written")
		}
	})

	t.Run("--dry-run prints the config and writes or installs nothing", func(t *testing.T) {
		e, _, installs := initEnv(t, "")
		if code := e.run("init", "--yes", "--dry-run"); code != 0 {
			t.Fatalf("exit = %d, stderr %q", code, e.stderr.String())
		}
		if !strings.Contains(e.stdout.String(), `"~/Develop"`) {
			t.Errorf("stdout lacks the config:\n%s", e.stdout.String())
		}
		if _, err := os.Stat(cfgPath(e)); err == nil {
			t.Error("dry run wrote the config")
		}
		if len(*installs) != 0 {
			t.Errorf("dry run installed: %+v", *installs)
		}
	})

	t.Run("without a terminal init needs --yes", func(t *testing.T) {
		e, _, _ := initEnv(t, "")
		if code := e.run("init"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if _, err := os.Stat(cfgPath(e)); err == nil {
			t.Error("config written")
		}
	})

	t.Run("input ending mid-way writes nothing", func(t *testing.T) {
		e, _, installs := initEnv(t, "1\n")
		if code := e.run("init"); code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
		if _, err := os.Stat(cfgPath(e)); err == nil {
			t.Error("config written")
		}
		if len(*installs) != 0 {
			t.Errorf("installed: %+v", *installs)
		}
	})
}
