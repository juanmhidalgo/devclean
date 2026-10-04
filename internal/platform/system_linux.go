//go:build linux

package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// journalTarget is the size `journalctl --vacuum-size` is told to shrink the
// journal to. The journal is only reported when it uses more than this.
const (
	journalTarget      = "500M"
	journalTargetBytes = 500 << 20
	snapDir            = "/var/lib/snapd/snaps"
)

// SystemItems lists disabled snap revisions and an oversized systemd journal.
// A missing or failing tool becomes a skip reason ("<tool>: ..."), in the
// style of the other collectors.
func (Linux) SystemItems(ctx context.Context) ([]SystemItem, []string) {
	var items []SystemItem
	var skips []string

	out, err := runTool(ctx, "snap", "list", "--all")
	if err != nil {
		skips = append(skips, "snap: "+err.Error())
	} else {
		items = append(items, parseSnapList(out)...)
	}

	out, err = runTool(ctx, "journalctl", "--disk-usage")
	if err != nil {
		skips = append(skips, "journalctl: "+err.Error())
	} else if n, ok := parseJournalUsage(out); !ok {
		skips = append(skips, "journalctl: cannot parse disk usage: "+strings.TrimSpace(out))
	} else if n > journalTargetBytes {
		items = append(items, SystemItem{
			Name:       "journal",
			Size:       n,
			Reason:     "systemd journal exceeds " + journalTarget,
			ReclaimCmd: "sudo journalctl --vacuum-size=" + journalTarget,
		})
	}
	return items, skips
}

func runTool(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), nil
	case errors.As(err, &exit):
		msg := string(bytes.TrimSpace(out))
		if msg == "" {
			msg = "no output"
		}
		return "", fmt.Errorf("exit status %d: %s", exit.ExitCode(), msg)
	case errors.Is(err, exec.ErrNotFound):
		return "", errors.New("not installed")
	default:
		return "", err
	}
}

// parseSnapList returns the disabled revisions in `snap list --all` output.
func parseSnapList(out string) []SystemItem {
	var items []SystemItem
	for i, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) < 6 {
			continue
		}
		name, rev, notes := f[0], f[2], f[len(f)-1]
		if !strings.Contains(","+notes+",", ",disabled,") {
			continue
		}
		var size int64
		if fi, err := os.Lstat(filepath.Join(snapDir, name+"_"+rev+".snap")); err == nil {
			size = fi.Size()
		}
		items = append(items, SystemItem{
			Name:       name + " " + rev,
			Size:       size,
			Reason:     "disabled snap revision",
			ReclaimCmd: fmt.Sprintf("sudo snap remove %s --revision=%s", name, rev),
		})
	}
	return items
}

// parseJournalUsage extracts the byte count from "... take up 1.5G in ...".
func parseJournalUsage(out string) (int64, bool) {
	f := strings.Fields(out)
	for i, w := range f {
		if w != "up" || i+1 >= len(f) {
			continue
		}
		v := f[i+1]
		mult := int64(1)
		if k := strings.IndexByte("KMGT", v[len(v)-1]); k >= 0 {
			mult = 1 << (10 * (k + 1))
			v = v[:len(v)-1]
		}
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		return int64(x * float64(mult)), true
	}
	return 0, false
}
