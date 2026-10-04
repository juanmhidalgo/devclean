package history

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPruneByCoverage(t *testing.T) {
	root := "/home/a"
	tests := []struct {
		name string
		keys []string
		cov  []Coverage
		want []string // keys that must remain
	}{
		{
			name: "unseen entry under covered root is removed",
			keys: []string{"projects:/home/a/proj/x"},
			cov:  []Coverage{{Category: "projects", Roots: []string{root}, Complete: true}},
			want: nil,
		},
		{
			name: "seen entry is kept",
			keys: []string{"projects:/home/a/proj/x"},
			cov: []Coverage{{Category: "projects", Roots: []string{root}, Complete: true,
				Seen: map[string]bool{"projects:/home/a/proj/x": true}}},
			want: []string{"projects:/home/a/proj/x"},
		},
		{
			// MUTATION: ignore Roots -> the entry under /home/b is removed
			name: "other root is left intact",
			keys: []string{"projects:/home/b/proj/x"},
			cov:  []Coverage{{Category: "projects", Roots: []string{root}, Complete: true}},
			want: []string{"projects:/home/b/proj/x"},
		},
		{
			name: "sibling with common name prefix is not under root",
			keys: []string{"projects:/home/a/proj/x"},
			cov:  []Coverage{{Category: "projects", Roots: []string{"/home/a/pro"}, Complete: true}},
			want: []string{"projects:/home/a/proj/x"},
		},
		{
			// MUTATION: ignore Unreadable -> the entry under the unreadable path is removed
			name: "unreadable subtree is left intact",
			keys: []string{"projects:/home/a/locked/x", "projects:/home/a/open/x"},
			cov: []Coverage{{Category: "projects", Roots: []string{root},
				Unreadable: []string{"/home/a/locked"}, Complete: true}},
			want: []string{"projects:/home/a/locked/x"},
		},
		{
			// MUTATION: ignore Complete -> the docker entry is removed
			name: "daemon down removes no docker entry",
			keys: []string{"docker:sha256:abc"},
			cov:  []Coverage{{Category: "docker", Complete: false}},
			want: []string{"docker:sha256:abc"},
		},
		{
			name: "complete docker coverage removes unseen image only",
			keys: []string{"docker:sha256:abc", "docker:sha256:def"},
			cov: []Coverage{{Category: "docker", Complete: true,
				Seen: map[string]bool{"docker:sha256:def": true}}},
			want: []string{"docker:sha256:def"},
		},
		{
			name: "other category is untouched",
			keys: []string{"caches:/home/a/c", "docker:sha256:abc"},
			cov:  []Coverage{{Category: "docker", Complete: true}},
			want: []string{"caches:/home/a/c"},
		},
		{
			name: "incomplete path coverage prunes nothing",
			keys: []string{"projects:/home/a/x"},
			cov:  []Coverage{{Category: "projects", Roots: []string{root}, Complete: false}},
			want: []string{"projects:/home/a/x"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := empty()
			for _, k := range tc.keys {
				h.Entries[k] = Entry{FirstSeen: time.Unix(1, 0)}
			}
			got := Prune(h, tc.cov...)
			if len(got.Entries) != len(tc.want) {
				t.Fatalf("remaining = %v, want %v", got.Entries, tc.want)
			}
			for _, k := range tc.want {
				if _, ok := got.Entries[k]; !ok {
					t.Errorf("entry %q was removed, want kept", k)
				}
			}
		})
	}
}

func TestSavePrunesUnderLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h", "history.json")
	now := func() time.Time { return time.Unix(100, 0) }
	disk := empty()
	disk.Entries["docker:sha256:old"] = Entry{FirstSeen: time.Unix(1, 0)}
	if _, err := Save(path, disk, now); err != nil {
		t.Fatal(err)
	}
	cov := Coverage{Category: "docker", Complete: true}
	if _, err := Save(path, empty(), now, cov); err != nil {
		t.Fatal(err)
	}
	got, _, err := Load(path, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Entries["docker:sha256:old"]; ok {
		t.Errorf("pruned entry resurrected from disk: %v", got.Entries)
	}
}
