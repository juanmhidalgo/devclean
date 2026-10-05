package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/juanmhidalgo/devclean/internal/lockfile"
)

// Save merges h into the history file at path under an exclusive lock on
// <path>.lock and writes the result atomically (temp file in the same
// directory, fsync, rename). Per entry it keeps the max last_used and the min
// non-zero first_seen; per walked category the max time. The returned string
// is a warning when the on-disk file was corrupt and moved aside (see Load).
func Save(path string, h History, now func() time.Time, prune ...Coverage) (string, error) {
	if err := Open(path); err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	unlock, err := lockfile.Lock(path + ".lock")
	if err != nil {
		return "", err
	}
	defer func() { _ = unlock() }()

	disk, warning, err := Load(path, now)
	if err != nil {
		return "", err
	}
	merged := Prune(merge(disk, h), prune...)
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return warning, nil
}

func merge(a, b History) History {
	out := empty()
	for k, e := range a.Entries {
		out.Entries[k] = e
	}
	for k, e := range b.Entries {
		cur, ok := out.Entries[k]
		if !ok {
			out.Entries[k] = e
			continue
		}
		cur.FirstSeen = minTime(cur.FirstSeen, e.FirstSeen)
		if e.LastUsed.After(cur.LastUsed) {
			cur.LastUsed = e.LastUsed
		}
		out.Entries[k] = cur
	}
	for _, w := range []map[string]time.Time{a.Walked, b.Walked} {
		for k, t := range w {
			if t.After(out.Walked[k]) {
				out.Walked[k] = t
			}
		}
	}
	return out
}

// minTime returns the earlier of a and b, ignoring zero values.
func minTime(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}
