package remove

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/testenv"
)

func TestRemoveImage(t *testing.T) {
	const daemonMsg = "Error response from daemon: conflict: unable to delete abc (image is being used by stopped container 123)"
	tests := []struct {
		name    string
		script  string
		wantErr string
	}{
		{"success", "exit 0", ""},
		{"in use by stopped container", `echo "` + daemonMsg + `" >&2; exit 1`, "image is being used by stopped container"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argsFile := filepath.Join(t.TempDir(), "args")
			testenv.FakeBin(t, "docker", `for a in "$@"; do echo "$a" >> "`+argsFile+`"; done`+"\n"+tc.script)

			err := RemoveImage(context.Background(), "sha256:abc", nil)

			raw, rerr := os.ReadFile(argsFile)
			if rerr != nil {
				t.Fatalf("fake docker was not run: %v", rerr)
			}
			args := strings.Fields(string(raw))
			if got, want := strings.Join(args, " "), "image rm sha256:abc"; got != want {
				t.Errorf("args = %q, want %q", got, want)
			}
			for _, a := range args {
				// MUTATION: AC-28 — adding -f/--force to the docker args must fail this.
				if a == "-f" || a == "--force" {
					t.Errorf("args contain force flag %q", a)
				}
			}
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// A tagged image goes by its refs: `docker image rm <id>` is refused for an
// image referenced by several repositories unless forced (checked against a
// real daemon: "image is referenced in multiple repositories").
func TestRemoveImageByRefs(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	testenv.FakeBin(t, "docker", `for a in "$@"; do echo "$a" >> "`+argsFile+`"; done`)
	if err := RemoveImage(context.Background(), "sha256:abc", []string{"app:1", "other:2"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(strings.Fields(string(raw)), " "), "image rm app:1 other:2"; got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
}
