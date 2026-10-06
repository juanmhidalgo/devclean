package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/config"
)

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

// sized creates a sparse file of n bytes.
func sized(t *testing.T, p string, n int64) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(n); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFindRoots(t *testing.T) {
	home := t.TempDir()
	for _, p := range []string{
		"Develop/a/.git",
		"Develop/group/b/.git",
		"Develop/group/b/vendor/c/.git", // inside a repo: not counted
		"src/d/.git",
		".hidden/e/.git",
		"Music/album",
		"deep/1/2/3/f/.git", // beyond depth 3
	} {
		mkdir(t, filepath.Join(home, p))
	}
	if err := os.Symlink(filepath.Join(home, "Develop"), filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}

	got := FindRoots(home, 3)
	want := []Root{{filepath.Join(home, "Develop"), 2}, {filepath.Join(home, "src"), 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindRoots = %+v, want %+v", got, want)
	}
}

func TestFindDiskImages(t *testing.T) {
	home := t.TempDir()
	sized(t, filepath.Join(home, "vms/kali/kali.qcow2"), 300)
	sized(t, filepath.Join(home, "vms/kali/old.qcow2"), 200)
	sized(t, filepath.Join(home, "Downloads/ubuntu.ISO"), 400)
	sized(t, filepath.Join(home, "Downloads/small.iso"), 10)   // below minSize
	sized(t, filepath.Join(home, "Downloads/movie.mkv"), 900)  // not a disk image
	sized(t, filepath.Join(home, ".local/share/x.qcow2"), 900) // hidden dir
	sized(t, filepath.Join(home, "a/b/c/d/deep.qcow2"), 900)   // beyond depth 4
	sized(t, filepath.Join(home, "a/b/c/shallow.vmdk"), 100)   // at depth 4

	got := FindDiskImages(home, 100, 4)
	want := []WatchGroup{
		{Glob: filepath.Join(home, "vms/kali/*.qcow2"), Files: 2, Size: 500},
		{Glob: filepath.Join(home, "Downloads/*.ISO"), Files: 1, Size: 400},
		{Glob: filepath.Join(home, "a/b/c/*.vmdk"), Files: 1, Size: 100},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindDiskImages =\n%+v\nwant\n%+v", got, want)
	}
}

func TestTilde(t *testing.T) {
	for in, want := range map[string]string{
		"/home/u":         "~",
		"/home/u/Develop": "~/Develop",
		"/home/user2/x":   "/home/user2/x",
		"/srv/projects":   "/srv/projects",
	} {
		if got := Tilde("/home/u", in); got != want {
			t.Errorf("Tilde(%q) = %q, want %q", in, got, want)
		}
	}
}

// The rendered file must load back into exactly the answers, defaults kept.
func TestRenderRoundTrip(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.toml")
	load := func(a Answers) config.Config {
		t.Helper()
		if err := os.WriteFile(path, []byte(Render(a, config.Default(home))), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(path, home)
		if err != nil {
			t.Fatalf("rendered config does not load: %v\n%s", err, Render(a, config.Default(home)))
		}
		return cfg
	}

	t.Run("answers", func(t *testing.T) {
		cmd := `notify -t "dev clean" -T broom \ --tab	x`
		cfg := load(Answers{Roots: []string{"~/Develop", "/srv/p"}, Watch: []string{"~/vms/*.qcow2"}, NotifyCommand: cmd})
		if want := []string{filepath.Join(home, "Develop"), "/srv/p"}; !reflect.DeepEqual(cfg.Scan.Roots, want) {
			t.Errorf("roots = %v, want %v", cfg.Scan.Roots, want)
		}
		if want := []string{"~/vms/*.qcow2"}; !reflect.DeepEqual(cfg.Watch, want) {
			t.Errorf("watch = %v, want %v", cfg.Watch, want)
		}
		if cfg.NotifyCommand != cmd {
			t.Errorf("notify_command = %q, want %q", cfg.NotifyCommand, cmd)
		}
	})

	t.Run("no answers keeps every default", func(t *testing.T) {
		cfg := load(Answers{})
		if def := config.Default(home); !reflect.DeepEqual(cfg, def) {
			t.Errorf("config = %+v\nwant defaults %+v", cfg, def)
		}
	})
}
