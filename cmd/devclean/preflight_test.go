package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/config"
	"github.com/juanmhidalgo/devclean/internal/history"
	"github.com/juanmhidalgo/devclean/internal/platform"
)

type fakePlatform struct {
	supported                              error
	euid                                   int
	configDir, stateDir, cacheDir, dataDir string
	// statfs, when set, answers Statfs; otherwise it returns zero usage.
	statfs func(path string) platform.FSUsage
	// installs and uninstalls record schedule calls when non-nil.
	installs   *[]platform.ScheduleSpec
	uninstalls *int
	// scheduleErr is returned by the schedule methods.
	scheduleErr error
	// mountPoint is the Point MountFor reports for every path.
	mountPoint string
}

func (f fakePlatform) InstallSchedule(s platform.ScheduleSpec) error {
	if f.installs != nil {
		*f.installs = append(*f.installs, s)
	}
	return f.scheduleErr
}

func (f fakePlatform) UninstallSchedule() error {
	if f.uninstalls != nil {
		*f.uninstalls++
	}
	return f.scheduleErr
}

func (f fakePlatform) ConfigDir() string { return f.configDir }
func (f fakePlatform) StateDir() string  { return f.stateDir }
func (f fakePlatform) CacheDir() string  { return f.cacheDir }
func (f fakePlatform) DataDir() string   { return f.dataDir }
func (f fakePlatform) Supported() error  { return f.supported }
func (f fakePlatform) Euid() int         { return f.euid }
func (f fakePlatform) MountFor(string) (platform.Mount, error) {
	return platform.Mount{Point: f.mountPoint}, nil
}

func (f fakePlatform) Statfs(p string) (platform.FSUsage, error) {
	if f.statfs != nil {
		return f.statfs(p), nil
	}
	return platform.FSUsage{}, nil
}

func (fakePlatform) SystemItems(context.Context) ([]platform.SystemItem, []string) {
	return nil, nil
}

func TestPreflight(t *testing.T) {
	tests := []struct {
		name     string
		plat     fakePlatform
		wantCode int
		wantMsg  string
	}{
		{"unsupported platform (AC-10)", fakePlatform{supported: platform.ErrUnsupported, euid: 1000}, 1, "unsupported platform"},
		{"root refused (AC-40)", fakePlatform{euid: 0}, 1, "refusing to run as root"},
		{"ok", fakePlatform{euid: 1000}, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			calls := 0
			a := &app{
				platform: tt.plat,
				stderr:   &stderr,
				newCollectors: func(config.Config, history.History) []collect.Collector {
					calls++
					return nil
				},
			}
			if got := a.preflight(); got != tt.wantCode {
				t.Fatalf("exit code = %d, want %d", got, tt.wantCode)
			}
			if !strings.Contains(stderr.String(), tt.wantMsg) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantMsg)
			}
			if tt.wantCode != 0 && calls != 0 {
				t.Errorf("collectors invoked %d times, want 0", calls)
			}
		})
	}
}
