package history

import (
	"path/filepath"
	"strings"
)

// Coverage describes what one collector looked at during a run.
type Coverage struct {
	Category   string          // entry category this coverage speaks for
	Roots      []string        // covered roots (path categories only)
	Unreadable []string        // paths that could not be read; nothing under them is pruned
	Complete   bool            // false when the collector could not see everything (e.g. daemon down)
	Seen       map[string]bool // full entry keys ("<category>:<key>") seen this run
}

// dockerCategory is the one category whose keys are not paths.
const dockerCategory = "docker"

// Prune returns a copy of h without the stale entries. An entry is removed
// only when a coverage matches its category, that coverage is Complete, the
// entry was not in its Seen set, and (path categories) it lies under a covered
// root and under no Unreadable path. Incomplete coverage prunes nothing, for
// path categories as well as docker. Containment is component-wise.
func Prune(h History, cov ...Coverage) History {
	out := h
	out.Entries = make(map[string]Entry, len(h.Entries))
	for k, e := range h.Entries {
		if !stale(k, cov) {
			out.Entries[k] = e
		}
	}
	return out
}

func stale(key string, cov []Coverage) bool {
	for _, c := range cov {
		prefix := c.Category + ":"
		if !strings.HasPrefix(key, prefix) || !c.Complete || c.Seen[key] {
			continue
		}
		if c.Category == dockerCategory {
			return true
		}
		p := strings.TrimPrefix(key, prefix)
		if underAny(p, c.Roots) && !underAny(p, c.Unreadable) {
			return true
		}
	}
	return false
}

func underAny(p string, dirs []string) bool {
	for _, d := range dirs {
		rel, err := filepath.Rel(d, p)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
