package history

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/testenv"
)

// testRealHome is captured at package init, before any t.Setenv.
var testRealHome = os.Getenv("HOME")

func TestOpenRefusesRealHome(t *testing.T) {
	t.Run("ac42_real_home_refused", func(t *testing.T) {
		if testRealHome == "" {
			t.Skip("HOME not set")
		}
		err := Open(filepath.Join(testRealHome, "some", "dir", "history.json"))
		if !errors.Is(err, ErrRealHomeUnderTest) {
			t.Fatalf("Open under real HOME = %v, want ErrRealHomeUnderTest", err)
		}
	})

	t.Run("pin_isolate_sets_distinct_xdg_dirs", func(t *testing.T) {
		state, config := testenv.Isolate(t)
		if state == config {
			t.Fatalf("state and config dirs are equal: %q", state)
		}
		if got := os.Getenv("XDG_STATE_HOME"); got != state {
			t.Errorf("XDG_STATE_HOME = %q, want %q", got, state)
		}
		if got := os.Getenv("XDG_CONFIG_HOME"); got != config {
			t.Errorf("XDG_CONFIG_HOME = %q, want %q", got, config)
		}
		if !strings.HasPrefix(state, filepath.Dir(state)) || filepath.Dir(state) != filepath.Dir(config) {
			t.Errorf("dirs not under one temp root: %q %q", state, config)
		}
	})

	t.Run("pin_open_succeeds_inside_isolated_dirs", func(t *testing.T) {
		state, config := testenv.Isolate(t)
		for _, dir := range []string{state, config} {
			if err := Open(filepath.Join(dir, "devclean", "history.json")); err != nil {
				t.Errorf("Open(%q) = %v, want nil", dir, err)
			}
		}
	})
}
