package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/notify"
	"github.com/juanmhidalgo/devclean/internal/report"
	"github.com/juanmhidalgo/devclean/internal/setup"
)

const (
	// rootDepth is how far below a home subdirectory init looks for repos.
	rootDepth = 3
	// imageDepth and imageMinSize bound the disk image search under home.
	imageDepth   = 4
	imageMinSize = 1 << 30
	// desktopNotify is the suggested notify_command when notify-send exists.
	desktopNotify = `notify-send devclean "$(cat)"`
)

// errInputEnded aborts init when stdin closes mid-way; nothing is written.
var errInputEnded = errors.New("input ended before init finished; nothing was written")

type initOptions struct {
	yes           bool
	dryRun        bool
	force         bool
	notifyCommand string
}

func (a *app) newInitCmd(code *int) *cobra.Command {
	var o initOptions
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a first config from what is found on this machine",
		Long: "init looks for the directories holding your git repositories and for large\n" +
			"disk images, asks which to use and how to notify you, writes the config file\n" +
			"and offers to schedule devclean in trial mode (report only, deletes nothing).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			*code = a.runInit(cmd.Context(), o)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.yes, "yes", false, "accept everything found and install the trial schedule, without asking")
	f.BoolVar(&o.dryRun, "dry-run", false, "print the config instead of writing it; install nothing")
	f.BoolVar(&o.force, "force", false, "replace an existing config file (kept as config.toml.bak)")
	f.StringVar(&o.notifyCommand, "notify-command", "", "notify_command to write (body arrives on stdin)")
	return cmd
}

func (a *app) runInit(ctx context.Context, o initOptions) int {
	if code := a.preflight(); code != 0 {
		return code
	}
	if !o.yes && (!a.tty() || a.stdin == nil) {
		return a.fatal(errors.New("init asks questions: run it in a terminal, or pass --yes"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return a.fatal(err)
	}
	path := filepath.Join(a.platform.ConfigDir(), "config.toml")
	_, statErr := os.Stat(path)
	exists := statErr == nil
	if exists && !o.force && !o.dryRun {
		return a.fatal(fmt.Errorf("%s already exists; --force replaces it (the old one is kept as config.toml.bak)", path))
	}

	p := &prompter{a: a, yes: o.yes}
	if a.stdin != nil {
		p.in = bufio.NewReader(a.stdin)
	}
	ans := setup.Answers{NotifyCommand: o.notifyCommand}
	if ans.Roots, err = p.chooseRoots(home); err != nil {
		return a.fatal(err)
	}
	if ans.Watch, err = p.chooseWatch(home); err != nil {
		return a.fatal(err)
	}
	if !o.yes && o.notifyCommand == "" {
		if ans.NotifyCommand, err = p.askNotify(ctx, !o.dryRun); err != nil {
			return a.fatal(err)
		}
	}

	text := setup.Render(ans, config.Default(home))
	if o.dryRun {
		a.stdoutf("\n# %s (dry run: nothing written)\n%s", path, text)
		return 0
	}
	if err := writeConfig(path, text, home, exists); err != nil {
		return a.fatal(err)
	}
	a.stdoutf("\nWrote %s\n", path)
	if exists {
		a.stdoutf("The previous config is in %s.bak\n", path)
	}

	ok, err := p.confirm("Schedule devclean in trial mode (hourly observe, a periodic report; deletes nothing)?")
	if err != nil {
		return a.fatal(err)
	}
	if ok {
		if code := a.runScheduleInstall(true); code != 0 {
			return code
		}
		a.stdoutf("Scheduled.\n")
	}
	ok, err = p.confirm("Record a first usage observation now (takes a few seconds)?")
	if err != nil {
		return a.fatal(err)
	}
	if ok {
		if code := a.runObserve(ctx, runOptions{color: "never"}); code != 0 {
			return code
		}
	}
	a.stdoutf("\nNext: `devclean report` shows what can be reclaimed. Once the reports look\n" +
		"right, `devclean schedule install` switches to unattended cleaning.\n")
	return 0
}

// writeConfig validates text as a config, then puts it at path through a
// temp file and a rename. An existing file is first moved to path.bak.
func writeConfig(path, text, home string, exists bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "config.toml.tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, err := config.Load(tmp.Name(), home); err != nil {
		return fmt.Errorf("generated config is invalid (a bug in init): %w", err)
	}
	if exists {
		if err := os.Rename(path, path+".bak"); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), path)
}

// prompter asks init's questions; with yes it answers each with its default.
type prompter struct {
	a   *app
	in  *bufio.Reader
	yes bool
}

// line prints prompt and reads one answer. A closed stdin is errInputEnded.
func (p *prompter) line(prompt string) (string, error) {
	p.a.stdoutf("%s", prompt)
	s, err := p.in.ReadString('\n')
	if err != nil && (err != io.EOF || s == "") {
		p.a.stdoutf("\n")
		return "", errInputEnded
	}
	return strings.TrimSpace(s), nil
}

// confirm asks a yes/no question whose default is yes.
func (p *prompter) confirm(question string) (bool, error) {
	if p.yes {
		return true, nil
	}
	for i := 0; i < maxPromptTries; i++ {
		s, err := p.line("\n" + question + " [Y/n]: ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(s) {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
	return false, nil
}

// pick lists items numbered and returns the chosen ones; all by default.
func (p *prompter) pick(items []string) ([]int, error) {
	all, _ := report.ParseSelection("all", len(items))
	for i, it := range items {
		p.a.stdoutf("  %d. %s\n", i+1, it)
	}
	if p.yes {
		return all, nil
	}
	for i := 0; i < maxPromptTries; i++ {
		s, err := p.line("Use which? [all/none/1,3-5] (all): ")
		if err != nil {
			return nil, err
		}
		if s == "" {
			return all, nil
		}
		sel, perr := report.ParseSelection(s, len(items))
		if perr == nil {
			return sel, nil
		}
		p.a.stderrf("devclean: %v\n", perr)
	}
	return nil, errors.New("too many invalid answers; nothing was written")
}

func (p *prompter) chooseRoots(home string) ([]string, error) {
	roots := setup.FindRoots(home, rootDepth)
	p.a.stdoutf("Scan roots: where your projects live.\n")
	if len(roots) == 0 {
		p.a.stdoutf("  No git repositories found below your home's directories; devclean will\n" +
			"  scan all of home (edit [scan] roots in the config to narrow it).\n")
		return nil, nil
	}
	items := make([]string, len(roots))
	for i, r := range roots {
		items[i] = fmt.Sprintf("%-28s %s", setup.Tilde(home, r.Path), countOf(r.Repos, "git repo"))
	}
	sel, err := p.pick(items)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, i := range sel {
		out = append(out, setup.Tilde(home, roots[i-1].Path))
	}
	return out, nil
}

func (p *prompter) chooseWatch(home string) ([]string, error) {
	groups := setup.FindDiskImages(home, imageMinSize, imageDepth)
	if len(groups) == 0 {
		return nil, nil
	}
	p.a.stdoutf("\nLarge disk images, to list with their rm command (never deleted):\n")
	items := make([]string, len(groups))
	for i, g := range groups {
		items[i] = fmt.Sprintf("%-28s %s, %s", setup.Tilde(home, g.Glob), countOf(g.Files, "file"), report.FormatSize(g.Size))
	}
	sel, err := p.pick(items)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, i := range sel {
		out = append(out, setup.Tilde(home, groups[i-1].Glob))
	}
	return out, nil
}

// countOf writes n and noun, pluralized with an "s".
func countOf(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// askNotify asks for notify_command and, when test is set, sends a test
// notification, asking again if it fails or does not arrive. Empty means
// no notifications.
func (p *prompter) askNotify(ctx context.Context, test bool) (string, error) {
	p.a.stdoutf("\nNotifications: a command that receives each message on stdin " +
		"(empty for none).\n")
	if _, err := exec.LookPath("notify-send"); err == nil {
		p.a.stdoutf("  For desktop notifications: %s\n", desktopNotify)
	}
	for i := 0; i < maxPromptTries; i++ {
		cmd, err := p.line("Command: ")
		if err != nil || cmd == "" || !test {
			return cmd, err
		}
		ok, err := p.confirm("Send a test notification?")
		if err != nil || !ok {
			return cmd, err
		}
		if serr := notify.Send(ctx, cmd, "devclean: test notification\n"); serr != nil {
			p.a.stderrf("devclean: test notification failed: %v\n", serr)
			continue
		}
		// Some helpers exit 0 even when the server refused the message.
		arrived, err := p.confirm("Did it arrive?")
		if err != nil || arrived {
			return cmd, err
		}
	}
	return "", errors.New("no working notification command; nothing was written (rerun init, or leave the command empty)")
}
