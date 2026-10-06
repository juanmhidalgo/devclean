// Package platform isolates everything OS-specific behind one interface.
package platform

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
)

// ErrUnsupported is returned by Supported on platforms devclean cannot run on.
var ErrUnsupported = errors.New("unsupported platform")

// Platform exposes the OS-specific services devclean needs.
type Platform interface {
	// ConfigDir is the directory holding devclean's configuration.
	ConfigDir() string
	// StateDir is the directory holding devclean's history and state.
	StateDir() string
	// CacheDir is the base directory of the user's caches.
	CacheDir() string
	// DataDir is the base directory of the user's data files.
	DataDir() string
	// Supported returns nil when devclean can run here, else ErrUnsupported.
	Supported() error
	// Euid is the effective user id of the process.
	Euid() int
	// MountFor returns the mount that holds path.
	MountFor(path string) (Mount, error)
	// Statfs reports usage of the filesystem holding path. The returned ID
	// is the mount Device (major:minor), the same value collectors put in
	// Candidate.FSID.
	Statfs(path string) (FSUsage, error)
	// SystemItems reports system-level reclaimable items (superseded
	// packages, logs). Each carries the exact command the user would run;
	// devclean never runs them. Sources that cannot be read come back as
	// human-readable skip reasons, never silently dropped.
	SystemItems(ctx context.Context) (items []SystemItem, skips []string)
	// InstallSchedule installs and enables the recurring jobs described by
	// spec: an hourly observe and a periodic clean (or report).
	InstallSchedule(spec ScheduleSpec) error
	// UninstallSchedule disables and removes the recurring jobs.
	UninstallSchedule() error
}

// ScheduleSpec describes the recurring jobs to install.
type ScheduleSpec struct {
	// Binary is the absolute path of the devclean executable to run.
	Binary string
	// PATH is the explicit PATH the jobs run with.
	PATH string
	// CleanCadence is the calendar expression for the clean job, e.g. "weekly".
	CleanCadence string
	// ReportOnly installs the clean job as a read-only report (trial mode).
	ReportOnly bool
	// Notify makes the report-only job send its result to notify_command.
	// The clean job always notifies when notify_command is set.
	Notify bool
}

// FSUsage is a platform-neutral statfs result, df-like: Used excludes
// reserved blocks from Avail's side, Avail is what an unprivileged user
// can still write.
type FSUsage struct {
	ID    string
	Used  uint64
	Avail uint64
}

// SystemItem is one system-level reclaim opportunity, always manual.
type SystemItem struct {
	Name       string
	Size       int64
	Reason     string
	ReclaimCmd string
	// Tip says how to keep the item from growing back, when there is a way.
	Tip string
}

// Mount describes one mounted filesystem.
type Mount struct {
	Point string
	// Device is the major:minor device number, the filesystem identity.
	Device  string
	Options []string
	// NoAtime is true only when the mount options include "noatime";
	// relatime still updates access times.
	NoAtime bool
}

// FindMount returns the mount with the longest mount-point prefix of path,
// matching on path-component boundaries.
func FindMount(mounts []Mount, path string) (Mount, bool) {
	path = filepath.Clean(path)
	var best Mount
	found := false
	for _, m := range mounts {
		if !underMount(m.Point, path) {
			continue
		}
		if !found || len(m.Point) > len(best.Point) {
			best, found = m, true
		}
	}
	return best, found
}

func underMount(point, path string) bool {
	if point == "/" {
		return true
	}
	return path == point || strings.HasPrefix(path, point+"/")
}
