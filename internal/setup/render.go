package setup

import (
	"fmt"
	"strings"

	"github.com/juanmhidalgo/devclean/internal/config"
)

// Answers are the choices a first config records. Paths are written as
// given, so callers pass them in the "~/..." form (see Tilde).
type Answers struct {
	Roots         []string
	Watch         []string
	NotifyCommand string
}

// Render writes a config file holding the answers, with every other common
// key present as a commented-out default taken from defaults.
func Render(a Answers, defaults config.Config) string {
	var b strings.Builder
	b.WriteString("# devclean configuration, written by `devclean init`.\n")
	b.WriteString("# Every key is optional: a missing key keeps its default.\n\n")

	b.WriteString("# Command that receives each notification on stdin.\n")
	if a.NotifyCommand != "" {
		fmt.Fprintf(&b, "notify_command = %s\n\n", tomlString(a.NotifyCommand))
	} else {
		b.WriteString("# notify_command = \"notify-send devclean \\\"$(cat)\\\"\"\n\n")
	}

	b.WriteString("# Files and globs listed in the manual tier with the command to remove\n")
	b.WriteString("# them. devclean never deletes them.\n")
	if len(a.Watch) > 0 {
		writeList(&b, "watch", a.Watch)
	} else {
		b.WriteString("# watch = [\"~/vms/*.qcow2\"]\n")
	}
	b.WriteString("\n")

	b.WriteString("# Disk use, in percent, at which clean also wipes package caches.\n")
	fmt.Fprintf(&b, "# pressure_percent = %d\n\n", defaults.PressurePercent)

	b.WriteString("[scan]\n")
	b.WriteString("# Where your projects live; build artifacts are looked for under these.\n")
	if len(a.Roots) > 0 {
		writeList(&b, "roots", a.Roots)
	} else {
		b.WriteString("# roots = [\"~\"]  # the default: your whole home directory\n")
	}
	b.WriteString("\n")

	b.WriteString("# Days without use before a project artifact or Docker image is stale.\n")
	b.WriteString("# [thresholds]\n")
	fmt.Fprintf(&b, "# projects = %d\n", defaults.ThresholdDays("projects"))
	fmt.Fprintf(&b, "# docker = %d\n", defaults.ThresholdDays("docker"))
	return b.String()
}

func writeList(b *strings.Builder, key string, items []string) {
	fmt.Fprintf(b, "%s = [\n", key)
	for _, it := range items {
		fmt.Fprintf(b, "  %s,\n", tomlString(it))
	}
	b.WriteString("]\n")
}

// tomlString quotes s as a TOML basic string.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
