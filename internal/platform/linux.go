//go:build linux

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Linux is the Linux implementation of Platform.
type Linux struct {
	// UnitDir overrides where schedule units are written; empty means the
	// user unit directory under the XDG config home.
	UnitDir string
}

var _ Platform = Linux{}

// ConfigDir returns $XDG_CONFIG_HOME/devclean, or ~/.config/devclean.
func (Linux) ConfigDir() string {
	return filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "devclean")
}

// StateDir returns $XDG_STATE_HOME/devclean, or ~/.local/state/devclean.
func (Linux) StateDir() string {
	return filepath.Join(xdgDir("XDG_STATE_HOME", ".local/state"), "devclean")
}

// CacheDir returns $XDG_CACHE_HOME, or ~/.cache.
func (Linux) CacheDir() string {
	return xdgDir("XDG_CACHE_HOME", ".cache")
}

// DataDir returns $XDG_DATA_HOME, or ~/.local/share.
func (Linux) DataDir() string {
	return xdgDir("XDG_DATA_HOME", ".local/share")
}

// xdgDir returns the variable's value when it is an absolute path (the XDG
// spec treats relative values as invalid), else the fallback under the home
// directory.
func xdgDir(variable, fallback string) string {
	if v := os.Getenv(variable); filepath.IsAbs(v) {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

// Supported always succeeds: Linux is the supported platform.
func (Linux) Supported() error { return nil }

// Euid returns the effective user id of the process.
func (Linux) Euid() int { return os.Geteuid() }

// MountFor returns the mount holding path, read from the kernel mount table.
func (Linux) MountFor(path string) (Mount, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return Mount{}, err
	}
	defer f.Close()
	mounts, err := ParseMountinfo(f)
	if err != nil {
		return Mount{}, err
	}
	m, ok := FindMount(mounts, path)
	if !ok {
		return Mount{}, fmt.Errorf("no mount found for %s", path)
	}
	return m, nil
}

// Statfs returns df-like usage for the filesystem holding path, identified
// by its mount Device.
func (l Linux) Statfs(path string) (FSUsage, error) {
	m, err := l.MountFor(path)
	if err != nil {
		return FSUsage{}, err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return FSUsage{}, err
	}
	bsize := uint64(st.Bsize)
	return FSUsage{
		ID:    m.Device,
		Used:  (st.Blocks - st.Bfree) * bsize,
		Avail: st.Bavail * bsize,
	}, nil
}

// Current returns the Platform for the OS devclean was built for.
func Current() Platform { return Linux{} }
