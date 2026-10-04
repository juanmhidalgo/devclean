package notify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/testenv"
)

const fakeNotifier = `cat > "$OUT_STDIN"; printf '%s\n' "$@" > "$OUT_ARGV"`

func TestSendStdin(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"leading dash", "- disk almost full\n- second line\n"},
		{"leading double dash", "--title injected\n"},
		{"command substitution not executed", "$(touch PWNED) `touch PWNED2`\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testenv.FakeBin(t, "fakenotify", fakeNotifier)
			dir := t.TempDir()
			outStdin := filepath.Join(dir, "stdin")
			outArgv := filepath.Join(dir, "argv")
			t.Setenv("OUT_STDIN", outStdin)
			t.Setenv("OUT_ARGV", outArgv)
			t.Chdir(dir)

			if err := Send(context.Background(), "fakenotify --title x", tc.body); err != nil {
				t.Fatalf("Send: %v", err)
			}
			gotStdin, err := os.ReadFile(outStdin)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotStdin) != tc.body {
				t.Errorf("stdin = %q, want %q", gotStdin, tc.body)
			}
			gotArgv, err := os.ReadFile(outArgv)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotArgv) != "--title\nx\n" {
				t.Errorf("argv = %q, want only the configured args", gotArgv)
			}
			if strings.Contains(string(gotArgv), strings.TrimSpace(tc.body)) {
				t.Errorf("argv contains body: %q", gotArgv)
			}
			for _, f := range []string{"PWNED", "PWNED2"} {
				if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
					t.Errorf("body was executed: %s exists", f)
				}
			}
		})
	}
}

func TestSendStdinErrors(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		wantErr []string
	}{
		{"empty command is a no-op", "", nil},
		{"non-zero exit with output", "echo boom >&2; exit 3", []string{"exit status 3", "boom", "echo boom >&2; exit 3"}},
		{"non-zero exit without output", "exit 2", []string{"exit status 2", "no output"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Send(context.Background(), tc.cmd, "body")
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatal("err = nil, want error")
			}
			for _, w := range tc.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("err %q missing %q", err, w)
				}
			}
		})
	}
}
