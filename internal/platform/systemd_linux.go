//go:build linux

package platform

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	observeUnit = "devclean-observe"
	cleanUnit   = "devclean-clean"
)

// unitDir is where user units live: UnitDir when set, else
// $XDG_CONFIG_HOME/systemd/user.
func (l Linux) unitDir() string {
	if l.UnitDir != "" {
		return l.UnitDir
	}
	return filepath.Join(filepath.Dir(l.ConfigDir()), "systemd", "user")
}

func serviceUnit(desc, binary string, args []string, path string) string {
	cmd := []string{unitQuote(binary)}
	for _, a := range args {
		cmd = append(cmd, unitQuote(a))
	}
	return fmt.Sprintf(`[Unit]
Description=%s

[Service]
Type=oneshot
Environment=%s
ExecStart=%s
`, desc, unitQuote("PATH="+path), strings.Join(cmd, " "))
}

// unitQuote renders one value as a unit-file word. "%" is doubled so systemd
// does not read it as a specifier. A value with whitespace, a quote or a
// backslash is double-quoted with those escaped, so a path with spaces stays
// one word. "$" is left alone: systemd does not expand it in the executable
// path (checked with systemd-analyze verify) nor in Environment=, and the
// arguments are fixed words.
func unitQuote(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	if !strings.ContainsAny(s, " \t\"'\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// validateCalendar rejects a clean cadence systemd would not accept, before
// any unit is written. A newline would inject a line into the timer unit.
// Without systemd-analyze only the newline check runs.
func validateCalendar(expr string) error {
	if expr == "" || strings.ContainsAny(expr, "\n\r") {
		return fmt.Errorf("invalid clean_cadence %q", expr)
	}
	if _, err := exec.LookPath("systemd-analyze"); err != nil {
		return nil
	}
	if out, err := exec.Command("systemd-analyze", "calendar", expr).CombinedOutput(); err != nil {
		return fmt.Errorf("invalid clean_cadence %q: %s", expr, strings.TrimSpace(string(out)))
	}
	return nil
}

func timerUnit(desc, calendar string) string {
	return fmt.Sprintf(`[Unit]
Description=%s

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, desc, calendar)
}

// InstallSchedule writes the observe and clean service/timer units, reloads
// the user manager and enables both timers. The unattended clean passes no
// --yes: stale items are skipped, only garbage and pressured caches go.
//
// The cadence is validated before anything is written. If writing or
// enabling fails part way, the install is rolled back so no half-installed
// schedule (an active observe timer without the clean one) is left behind.
func (l Linux) InstallSchedule(spec ScheduleSpec) error {
	if err := validateCalendar(spec.CleanCadence); err != nil {
		return err
	}
	cleanArgs, cleanDesc := []string{"clean", "--quiet"}, "devclean scheduled clean"
	if spec.ReportOnly {
		cleanArgs, cleanDesc = []string{"report"}, "devclean scheduled report (trial mode)"
	}
	files := map[string]string{
		observeUnit + ".service": serviceUnit("devclean observe", spec.Binary, []string{"observe"}, spec.PATH),
		observeUnit + ".timer":   timerUnit("devclean hourly observe", "hourly"),
		cleanUnit + ".service":   serviceUnit(cleanDesc, spec.Binary, cleanArgs, spec.PATH),
		cleanUnit + ".timer":     timerUnit("devclean periodic clean", spec.CleanCadence),
	}
	dir := l.unitDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := l.installUnits(dir, files); err != nil {
		if rerr := l.rollback(dir); rerr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rerr)
		}
		return err
	}
	return nil
}

// rollback undoes a partial install. Unlike UninstallSchedule it does every
// step even when one fails, since some units may never have been enabled.
func (l Linux) rollback(dir string) error {
	var errs []error
	for _, u := range []string{observeUnit, cleanUnit} {
		errs = append(errs, systemctl("disable", "--now", u+".timer"))
	}
	for _, u := range []string{observeUnit, cleanUnit} {
		for _, ext := range []string{".service", ".timer"} {
			if err := os.Remove(filepath.Join(dir, u+ext)); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	errs = append(errs, systemctl("daemon-reload"))
	return errors.Join(errs...)
}

func (l Linux) installUnits(dir string, files map[string]string) error {
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	for _, u := range []string{observeUnit, cleanUnit} {
		if err := systemctl("enable", "--now", u+".timer"); err != nil {
			return err
		}
	}
	return nil
}

// UninstallSchedule disables both timers, removes the four unit files and
// reloads the user manager.
func (l Linux) UninstallSchedule() error {
	for _, u := range []string{observeUnit, cleanUnit} {
		if err := systemctl("disable", "--now", u+".timer"); err != nil {
			return err
		}
	}
	dir := l.unitDir()
	for _, u := range []string{observeUnit, cleanUnit} {
		for _, ext := range []string{".service", ".timer"} {
			if err := os.Remove(filepath.Join(dir, u+ext)); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return systemctl("daemon-reload")
}

func systemctl(args ...string) error {
	cmdline := append([]string{"--user"}, args...)
	out, err := exec.Command("systemctl", cmdline...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = "no output"
		}
		return fmt.Errorf("systemctl %s: %w: %s", strings.Join(cmdline, " "), err, text)
	}
	return nil
}
