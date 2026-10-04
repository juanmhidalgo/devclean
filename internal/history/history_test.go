package history

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/testenv"
)

func TestLoadCorrupt(t *testing.T) {
	fixedNow := func() time.Time { return time.Unix(1700000000, 0) }
	tests := []struct {
		name        string
		content     *string // nil = file missing
		wantMoved   bool
		wantEntries int
	}{
		{"ac22_invalid_json", ptr(`{"version":1,"entries":{`), true, 0},
		{"ac22_not_json_at_all", ptr(`garbage`), true, 0},
		{"ac22_unknown_version", ptr(`{"version":99,"entries":{"image:a":{}}}`), true, 0},
		{"ac22_partial_entries_not_kept", ptr(`{"version":1,"entries":{"image:a":{"first_seen":"2026-01-01T00:00:00Z"}},"walked":`), true, 0},
		{"ac22_missing_file", nil, false, 0},
		{"pin_valid_file_loads", ptr(`{"version":1,"entries":{"image:a":{"first_seen":"2026-01-01T00:00:00Z","last_used":"2026-02-01T00:00:00Z"}},"walked":{}}`), false, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, _ := testenv.Isolate(t)
			path := filepath.Join(state, "devclean", "history.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.content != nil {
				if err := os.WriteFile(path, []byte(*tc.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			h, warn, err := Load(path, fixedNow)
			if err != nil {
				t.Fatalf("Load returned error %v, want nil", err)
			}
			// MUTATION: corrupt does not abort, does not invent entries (AC-22)
			if len(h.Entries) != tc.wantEntries {
				t.Fatalf("entries = %d, want %d", len(h.Entries), tc.wantEntries)
			}
			corrupt := path + ".corrupt-1700000000"
			_, statErr := os.Stat(corrupt)
			_, origErr := os.Stat(path)
			if tc.wantMoved {
				if statErr != nil {
					t.Errorf("corrupt copy missing: %v", statErr)
				}
				if !errors.Is(origErr, os.ErrNotExist) {
					t.Errorf("original still present: %v", origErr)
				}
				if !strings.Contains(warn, path) || !strings.Contains(warn, corrupt) {
					t.Errorf("warning %q must name %q and %q", warn, path, corrupt)
				}
			} else {
				if statErr == nil {
					t.Errorf("unexpected corrupt file %s", corrupt)
				}
				if warn != "" {
					t.Errorf("warning = %q, want empty", warn)
				}
			}
		})
	}
}

func ptr(s string) *string { return &s }
