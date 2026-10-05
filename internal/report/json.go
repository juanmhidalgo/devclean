package report

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/remove"
)

// SchemaVersion is the integer version of the JSON document. Additive fields
// do not bump it.
const SchemaVersion = 1

type jsonDoc struct {
	SchemaVersion          int               `json:"schema_version"`
	Candidates             []jsonCandidate   `json:"candidates"`
	Skipped                []jsonSkip        `json:"skipped"`
	Warnings               []string          `json:"warnings"`
	Notices                []string          `json:"notices"`
	FreedBytesByFilesystem *map[string]int64 `json:"freed_bytes_by_filesystem,omitempty"`
}

type jsonSkip struct {
	Collector string `json:"collector"`
	Reason    string `json:"reason"`
}

// jsonCandidate: exactly one of path, image_id, volume, action is set. last_used and
// last_used_source are null when there is no use evidence.
type jsonCandidate struct {
	Category  string  `json:"category"`
	Tier      string  `json:"tier"`
	Path      *string `json:"path,omitempty"`
	ImageID   *string `json:"image_id,omitempty"`
	Action    *string `json:"action,omitempty"`
	Volume    *string `json:"volume,omitempty"`
	SizeBytes int64   `json:"size_bytes"`
	// SizeUnknown: size_bytes is 0 because the size was not measured.
	SizeUnknown    bool    `json:"size_unknown,omitempty"`
	LastUsed       *string `json:"last_used"`
	LastUsedSource *string `json:"last_used_source"`
	Reason         string  `json:"reason"`
	Command        string  `json:"command,omitempty"`
	// UsedBy: the containers that must be removed before the volume can be.
	UsedBy []string `json:"used_by,omitempty"`
	// Tip says how to keep the item from growing back.
	Tip string `json:"tip,omitempty"`
	// Index is the 1-based stale number matching the interactive prompt.
	Index         int     `json:"index,omitempty"`
	Outcome       string  `json:"outcome,omitempty"`
	OutcomeReason *string `json:"outcome_reason,omitempty"`
}

var categoryNames = map[classify.Category]string{
	classify.CategoryProjects: "projects",
	classify.CategoryDocker:   "docker",
	classify.CategoryVenvs:    "venvs",
	classify.CategoryCaches:   "caches",
	classify.CategorySystem:   "system",
	classify.CategoryWatch:    "watch",
}

var tierNames = map[classify.Tier]string{
	classify.TierGarbage: "garbage",
	classify.TierCaches:  "caches",
	classify.TierStale:   "stale",
	classify.TierManual:  "manual",
}

var sourceNames = map[classify.Signal]string{
	classify.SignalCommit:        "commit",
	classify.SignalHead:          "head",
	classify.SignalIndex:         "index",
	classify.SignalArtifactMTime: "artifact_mtime",
	classify.SignalMarkerATime:   "marker_atime",
	classify.SignalHistory:       "history",
	classify.SignalImageLastSeen: "image_last_seen",
	classify.SignalFirstSeen:     "first_seen",
}

var statusNames = map[remove.Status]string{
	remove.StatusDeleted: "deleted",
	remove.StatusSkipped: "skipped",
	remove.StatusFailed:  "failed",
}

// RenderJSON writes the machine-readable document (schema_version 1) followed
// by a newline. Outcomes are matched to candidates by Path.
func RenderJSON(w io.Writer, r Report) error {
	doc := jsonDoc{
		SchemaVersion: SchemaVersion,
		Candidates:    []jsonCandidate{},
		Skipped:       []jsonSkip{},
		Warnings:      orEmpty(r.Warnings),
		Notices:       orEmpty(r.Notices),
	}
	if r.Freed != nil {
		doc.FreedBytesByFilesystem = &r.Freed
	}
	outcomes := map[string]remove.Outcome{}
	for _, o := range r.Outcomes {
		outcomes[o.Candidate.Path] = o
	}
	stale := 0
	for _, c := range r.Candidates {
		jc := jsonCandidate{
			Category:    categoryNames[c.Category],
			Tier:        tierNames[c.Tier],
			SizeBytes:   c.Size,
			SizeUnknown: c.SizeUnknown,
			Reason:      c.Reason,
			Command:     c.ReclaimCmd,
			UsedBy:      c.UsedBy,
			Tip:         c.Tip,
		}
		p := c.Path
		switch {
		case c.Category == classify.CategoryDocker && strings.HasPrefix(p, "sha256:"):
			jc.ImageID = &p
		case isVolume(c):
			jc.Volume = &p
		case !strings.HasPrefix(p, "/"):
			jc.Action = &p
		default:
			jc.Path = &p
		}
		if !c.LastUse.At.IsZero() {
			s := c.LastUse.At.Format(time.RFC3339)
			jc.LastUsed = &s
			if n, ok := sourceNames[c.LastUse.Source]; ok {
				jc.LastUsedSource = &n
			}
		}
		if c.Tier == classify.TierStale {
			stale++
			jc.Index = stale
		}
		if o, ok := outcomes[c.Path]; ok {
			jc.Outcome = statusNames[o.Status]
			msg := o.Reason
			if o.Err != nil {
				msg = o.Err.Error()
			}
			if msg != "" {
				jc.OutcomeReason = &msg
			}
		}
		doc.Candidates = append(doc.Candidates, jc)
	}
	for _, s := range r.Skipped {
		doc.Skipped = append(doc.Skipped, jsonSkip{Collector: s.Collector, Reason: s.Reason})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
