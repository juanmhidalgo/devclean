//go:build darwin

package platform

import "context"

// Darwin is a stub: devclean does not support macOS yet.
type Darwin struct{}

var _ Platform = Darwin{}

// ConfigDir is unused on an unsupported platform.
func (Darwin) ConfigDir() string { return "" }

// StateDir is unused on an unsupported platform.
func (Darwin) StateDir() string { return "" }

// CacheDir is unused on an unsupported platform.
func (Darwin) CacheDir() string { return "" }

// DataDir is unused on an unsupported platform.
func (Darwin) DataDir() string { return "" }

// Supported always reports ErrUnsupported.
func (Darwin) Supported() error { return ErrUnsupported }

// Euid is unused on an unsupported platform.
func (Darwin) Euid() int { return -1 }

// MountFor is unused on an unsupported platform.
func (Darwin) MountFor(string) (Mount, error) { return Mount{}, ErrUnsupported }

// Statfs is unused on an unsupported platform.
func (Darwin) Statfs(string) (FSUsage, error) { return FSUsage{}, ErrUnsupported }

// SystemItems reports nothing on an unsupported platform.
func (Darwin) SystemItems(context.Context) ([]SystemItem, []string) { return nil, nil }

// InstallSchedule reports ErrUnsupported.
func (Darwin) InstallSchedule(ScheduleSpec) error { return ErrUnsupported }

// UninstallSchedule reports ErrUnsupported.
func (Darwin) UninstallSchedule() error { return ErrUnsupported }

// Current returns the Platform for the OS devclean was built for.
func Current() Platform { return Darwin{} }
