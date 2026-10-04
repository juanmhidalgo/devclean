package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Version is the only history.json schema version this build understands.
const Version = 1

// Entry records when an item was first seen and last used.
type Entry struct {
	FirstSeen time.Time `json:"first_seen"`
	LastUsed  time.Time `json:"last_used"`
}

// History is the in-memory form of history.json.
type History struct {
	Version int                  `json:"version"`
	Entries map[string]Entry     `json:"entries"`
	Walked  map[string]time.Time `json:"walked"`
}

func empty() History {
	return History{Version: Version, Entries: map[string]Entry{}, Walked: map[string]time.Time{}}
}

// Load reads the history file at path. A missing file yields an empty history
// and no warning. A file that is not valid JSON, or has an unknown version, is
// renamed to <path>.corrupt-<unix> and an empty history is returned together
// with a warning naming both paths; this is not an error (AC-22). now supplies
// the clock for the suffix.
func Load(path string, now func() time.Time) (History, string, error) {
	if err := Open(path); err != nil {
		return History{}, "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return empty(), "", nil
	}
	if err != nil {
		return History{}, "", err
	}
	var h History
	if jsonErr := json.Unmarshal(data, &h); jsonErr != nil || h.Version != Version {
		moved := fmt.Sprintf("%s.corrupt-%d", path, now().Unix())
		if err := os.Rename(path, moved); err != nil {
			return History{}, "", err
		}
		return empty(), fmt.Sprintf("history file %s is corrupt or has an unknown version; moved to %s and starting empty", path, moved), nil
	}
	if h.Entries == nil {
		h.Entries = map[string]Entry{}
	}
	if h.Walked == nil {
		h.Walked = map[string]time.Time{}
	}
	return h, "", nil
}
