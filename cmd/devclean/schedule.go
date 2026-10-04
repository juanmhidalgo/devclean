package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/juanmhidalgo/devclean/internal/platform"
)

// newScheduleCmd builds `schedule install|uninstall`. Both go through the
// platform layer only.
func (a *app) newScheduleCmd(code *int) *cobra.Command {
	sched := &cobra.Command{Use: "schedule", Short: "Install or remove the scheduled jobs"}
	var reportOnly bool
	install := &cobra.Command{
		Use:   "install",
		Short: "Schedule observe hourly and clean on the configured cadence",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			*code = a.runScheduleInstall(reportOnly)
			return nil
		},
	}
	install.Flags().BoolVar(&reportOnly, "report-only", false, "schedule report instead of clean (trial mode)")
	uninstall := &cobra.Command{
		Use:   "uninstall",
		Short: "Disable and remove the scheduled jobs",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			*code = a.runScheduleUninstall()
			return nil
		},
	}
	sched.AddCommand(install, uninstall)
	return sched
}

func (a *app) runScheduleInstall(reportOnly bool) int {
	if code := a.preflight(); code != 0 {
		return code
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return a.fatal(err)
	}
	executable := a.executable
	if executable == nil {
		executable = os.Executable
	}
	bin, err := executable()
	if err != nil {
		return a.fatal(err)
	}
	err = a.platform.InstallSchedule(platform.ScheduleSpec{
		Binary:       bin,
		PATH:         os.Getenv("PATH"),
		CleanCadence: cfg.CleanCadence,
		ReportOnly:   reportOnly,
	})
	if err != nil {
		return a.fatal(err)
	}
	return 0
}

func (a *app) runScheduleUninstall() int {
	if code := a.preflight(); code != 0 {
		return code
	}
	if err := a.platform.UninstallSchedule(); err != nil {
		return a.fatal(err)
	}
	return 0
}
