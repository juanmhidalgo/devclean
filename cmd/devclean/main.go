// Command devclean reports and reclaims disk space used by developer tooling.
package main

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/platform"
	"github.com/juanmhidalgo/devclean/internal/remove"
)

// app holds the injectable dependencies shared by every command.
type app struct {
	platform platform.Platform
	stdin    io.Reader
	stdout   io.Writer
	stderr   io.Writer
	isTTY    func() bool
	// stdoutIsTTY decides --color=auto; nil means not a terminal.
	stdoutIsTTY func() bool
	now         func() time.Time
	// newCollectors builds the six collectors for one run.
	newCollectors func(cfg config.Config, hist history.History) []collect.Collector
	// newExecutor builds the deleting executor for clean; nil means the real
	// one. Tests inject recording hooks here.
	newExecutor func(revalidators map[string]func(context.Context) classify.Decision) *remove.Executor
	// executable returns the absolute path of the running binary; nil means
	// os.Executable. Tests inject a fixed path.
	executable func() (string, error)
}

// execute runs the command tree with args and returns the process exit code.
func (a *app) execute(args []string) int {
	code := 0
	root := newRootCmd(a, &code)
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	if err := root.Execute(); err != nil {
		return a.fatal(err)
	}
	return code
}

// newRootCmd builds the command tree. Running `devclean` bare prints the
// help. Each command stores its exit code in *code.
func newRootCmd(a *app, code *int) *cobra.Command {
	var opts runOptions
	root := &cobra.Command{
		Use:           "devclean",
		Short:         "Report and reclaim disk space used by developer tooling",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	pf := root.PersistentFlags()
	pf.StringSliceVar(&opts.only, "only", nil, "limit to these categories (repeatable): "+strings.Join(collectorNames, ", "))
	pf.StringSliceVar(&opts.tiers, "tier", nil, "limit to these tiers (repeatable): garbage, caches, stale, manual")
	pf.StringSliceVar(&opts.roots, "root", nil, "scan this root instead of the configured ones (repeatable)")
	pf.BoolVar(&opts.json, "json", false, "emit one JSON document")
	pf.StringVar(&opts.color, "color", "auto", "color the output: auto, always or never")
	var ro reportOptions
	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Show what could be reclaimed; deletes nothing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*code = a.runReport(cmd.Context(), opts, ro)
			return nil
		},
	}
	reportCmd.Flags().BoolVar(&ro.summary, "summary", false, "show only totals per tier and category")
	reportCmd.Flags().BoolVar(&ro.notify, "notify", false, "send the summary to notify_command")
	reportCmd.Flags().BoolVar(&ro.quiet, "quiet", false, "with --notify: notify only when something needs attention")
	root.AddCommand(reportCmd)
	var co cleanOptions
	clean := &cobra.Command{
		Use:   "clean",
		Short: "Delete garbage, pressure-driven caches and chosen stale items",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			co.runOptions = opts
			*code = a.runClean(cmd.Context(), co)
			return nil
		},
	}
	cf := clean.Flags()
	cf.BoolVar(&co.dryRun, "dry-run", false, "print what clean would delete; delete and record nothing")
	cf.BoolVar(&co.yes, "yes", false, "delete stale items without asking")
	cf.BoolVar(&co.quiet, "quiet", false, "notify only when something needs attention")
	root.AddCommand(clean)
	root.AddCommand(&cobra.Command{
		Use:   "observe",
		Short: "Record usage observations for the history; deletes nothing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*code = a.runObserve(cmd.Context(), opts)
			return nil
		},
	})
	root.AddCommand(a.newInitCmd(code))
	root.AddCommand(a.newScheduleCmd(code))
	return root
}

func main() {
	plat := platform.Current()
	a := &app{
		platform:      plat,
		stdin:         os.Stdin,
		isTTY:         stdinIsTTY,
		stdoutIsTTY:   func() bool { return isCharDevice(os.Stdout) },
		stdout:        os.Stdout,
		stderr:        os.Stderr,
		now:           time.Now,
		newCollectors: realCollectors(plat, time.Now),
	}
	os.Exit(a.execute(os.Args[1:]))
}

// stdinIsTTY reports whether stdin is a terminal.
func stdinIsTTY() bool { return isCharDevice(os.Stdin) }

// isCharDevice reports whether f is a character device (a terminal).
func isCharDevice(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
