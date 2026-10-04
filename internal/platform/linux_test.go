//go:build linux

package platform

import (
	"path/filepath"
	"testing"
)

func TestLinuxDirs(t *testing.T) {
	home := t.TempDir()
	abs := filepath.Join(t.TempDir(), "x")
	tests := []struct {
		name                          string
		config, state, cache, data    string // env values
		wantConfig, wantState, wantCa string
		wantData                      string
	}{
		{"absolute vars", abs + "/c", abs + "/s", abs + "/k", abs + "/d",
			abs + "/c/devclean", abs + "/s/devclean", abs + "/k", abs + "/d"},
		{"unset falls back", "", "", "", "",
			home + "/.config/devclean", home + "/.local/state/devclean", home + "/.cache", home + "/.local/share"},
		{"relative falls back", "rel/c", "rel/s", "rel/k", "rel/d",
			home + "/.config/devclean", home + "/.local/state/devclean", home + "/.cache", home + "/.local/share"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", tc.config)
			t.Setenv("XDG_STATE_HOME", tc.state)
			t.Setenv("XDG_CACHE_HOME", tc.cache)
			t.Setenv("XDG_DATA_HOME", tc.data)
			var p Platform = Linux{}
			if got := p.ConfigDir(); got != tc.wantConfig {
				t.Errorf("ConfigDir() = %q, want %q", got, tc.wantConfig)
			}
			if got := p.StateDir(); got != tc.wantState {
				t.Errorf("StateDir() = %q, want %q", got, tc.wantState)
			}
			if got := p.CacheDir(); got != tc.wantCa {
				t.Errorf("CacheDir() = %q, want %q", got, tc.wantCa)
			}
			if got := p.DataDir(); got != tc.wantData {
				t.Errorf("DataDir() = %q, want %q", got, tc.wantData)
			}
		})
	}
}

func TestLinuxStatfs(t *testing.T) {
	got, err := Linux{}.Statfs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" || got.Avail == 0 {
		t.Errorf("Statfs = %+v, want non-empty ID and Avail > 0", got)
	}
}
