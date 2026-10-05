// Package classify holds the pure classification rules. It does no I/O.
package classify

import "time"

// Tier is the cleanup tier an item falls into.
type Tier int

const (
	TierNone Tier = iota
	TierGarbage
	TierCaches
	TierStale
	TierManual
)

// Read is one observed atime read of an artifact.
type Read struct {
	ArtifactID string
	At         time.Time
}

// Signal is the source of an artifact's last-use evidence.
type Signal int

const (
	SignalNone Signal = iota
	SignalCommit
	SignalHead
	SignalIndex
	SignalArtifactMTime
	SignalMarkerATime
	SignalHistory
	SignalImageLastSeen
	SignalFirstSeen
)

// LastUse is the newest use evidence and the signal it came from.
type LastUse struct {
	At     time.Time
	Source Signal
}

// Decision is the outcome of classifying one item.
type Decision struct {
	Tier    Tier
	Reason  string
	LastUse LastUse
}

// ArtifactFacts are the pre-gathered signals for one project artifact.
// Zero times mean "signal missing" and are ignored. MarkerATimes are
// already burst-filtered by the caller.
type ArtifactFacts struct {
	InGitWorkTree   bool
	NoAtime         bool
	CommitTime      time.Time
	HeadMTime       time.Time
	IndexMTime      time.Time
	ArtifactMTime   time.Time
	MarkerATimes    []time.Time
	HistoryLastUsed time.Time
}

// Category is the kind of thing a candidate is.
type Category int

const (
	CategoryNone Category = iota
	CategoryProjects
	CategoryDocker
	CategoryVenvs
	CategoryCaches
	CategorySystem
	CategoryWatch
)

// Candidate is one classified item that may be deleted or reported.
type Candidate struct {
	Category Category
	Tier     Tier
	// Path is the filesystem path, or the image ID for docker items.
	Path string
	// Refs are a docker image's repo:tag references. An image with several
	// tags cannot be removed by ID without force, so it is removed by these.
	Refs []string
	// UsedBy are the containers, running or not, that mount a docker
	// volume. Docker refuses to remove the volume until they are removed.
	UsedBy []string
	Size   int64
	// SizeUnknown means Size was not measured (a tool-native prune, or a
	// volume docker did not size): 0 then does not mean empty.
	SizeUnknown bool
	// FSID identifies the filesystem the item lives on, to match FSStats.
	FSID    string
	Reason  string
	LastUse LastUse
	// ReclaimCmd is the command the user runs for manual items.
	ReclaimCmd string
	// Tip says how to keep the item from growing back; the report prints
	// each distinct tip once.
	Tip string
}
