package notify

// Summary is the minimal run outcome the quiet rule needs.
type Summary struct {
	AbovePressure     bool // a filesystem is at or above pressure
	FailedDeletions   int
	SkippedCollectors int
	Fatal             bool
}

// ShouldNotify reports whether a notification must be sent. Without quiet it
// is always true; with quiet only when something needs attention.
func ShouldNotify(s Summary, quiet bool) bool {
	if !quiet {
		return true
	}
	return s.AbovePressure || s.FailedDeletions > 0 || s.SkippedCollectors > 0 || s.Fatal
}
