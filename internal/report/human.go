// Package report renders scan and clean results for people and machines.
package report

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/juanmhidalgo/devclean/internal/classify"
	"github.com/juanmhidalgo/devclean/internal/collect"
	"github.com/juanmhidalgo/devclean/internal/remove"
)

// Report is the model shared by the report renderers.
type Report struct {
	Candidates []classify.Candidate
	Skipped    []collect.Skip
	Warnings   []string
	Notices    []string
	// Outcomes and Freed are set only by clean; Freed non-nil marks a clean
	// document even when nothing was freed.
	Outcomes []remove.Outcome
	Freed    map[string]int64
	// Now is the reference time for "days ago".
	Now time.Time
	// Home, when set, is shortened to ~ in the paths people read.
	Home string
	// Color enables ANSI colors in the human renderings.
	Color bool
}

var tierOrder = []struct {
	tier  classify.Tier
	title string
}{
	{classify.TierGarbage, "Garbage"},
	{classify.TierCaches, "Caches"},
	{classify.TierStale, "Stale"},
	{classify.TierManual, "Manual"},
}

var signalNames = map[classify.Signal]string{
	classify.SignalCommit:        "commit",
	classify.SignalHead:          "HEAD",
	classify.SignalIndex:         "index",
	classify.SignalArtifactMTime: "artifact mtime",
	classify.SignalMarkerATime:   "marker atime",
	classify.SignalHistory:       "history",
	classify.SignalImageLastSeen: "image last seen",
	classify.SignalFirstSeen:     "first seen",
}

// RenderHuman writes the plain-text report. Within a tier, items that share a
// reason are grouped under it, so the reason is printed once. Stale items are
// numbered 1-based in input order, matching classify.PlanClean's selection
// numbering, and keep that order; the other tiers list the largest first.
// Manual items show the command that reclaims them, ready to copy.
func RenderHuman(w io.Writer, r Report) error {
	ew := &errWriter{w: w}
	st := style{on: r.Color}
	var reclaimable, manual int64
	for _, c := range r.Candidates {
		switch c.Tier {
		case classify.TierGarbage, classify.TierCaches, classify.TierStale:
			reclaimable += c.Size
		case classify.TierManual:
			manual += c.Size
		}
	}
	for _, t := range tierOrder {
		var tierItems []item
		var total int64
		for _, c := range r.Candidates {
			if c.Tier == t.tier {
				tierItems = append(tierItems, item{c: c, n: len(tierItems) + 1})
				total += c.Size
			}
		}
		if len(tierItems) == 0 {
			ew.printf("%s %s\n\n", st.tier(t.tier, t.title+":"), st.dim("nothing to reclaim"))
			continue
		}
		ew.printf("%s %s\n", st.tier(t.tier, t.title), st.dim("— "+tierTotal(tierItems, total)))
		numbered := t.tier == classify.TierStale
		groups := groupByReason(tierItems, !numbered)
		v := view{home: r.Home, now: r.Now, manual: t.tier == classify.TierManual, st: st}
		if numbered {
			v.numWidth = len(fmt.Sprint(len(tierItems)))
		}
		for _, g := range groups {
			if len(g) == 1 {
				v.nameWidth = max(v.nameWidth, min(len([]rune(v.name(g[0].c))), maxNameWidth))
			}
		}
		for i, g := range groups {
			if len(g) == 1 {
				note := g[0].c.Reason
				if u := usedBy(g[0].c); u != "" {
					note += "; " + u
				}
				ew.printf("%s\n", v.line("  ", g[0], note))
				continue
			}
			if i > 0 {
				ew.printf("\n")
			}
			v.writeGroup(ew, g)
		}
		ew.printf("\n")
	}
	if len(r.Candidates) > 0 {
		ew.printf("%s%s\n\n", st.bold("Reclaimable (garbage+caches+stale)  "+formatSize(reclaimable)),
			st.dim("   manual: "+formatSize(manual)))
	}
	writeTips(ew, st, r.Candidates)
	writeTrailer(ew, r)
	return ew.err
}

// maxNameWidth caps the column a singleton's reason is aligned to, so one
// long path does not push every other reason off screen.
const maxNameWidth = 48

// item is a candidate with its 1-based position in its tier.
type item struct {
	c classify.Candidate
	n int
}

// groupByReason splits items by reason, in order of first appearance. With
// bySize, items are sorted largest first, single-item groups come first, and
// groups are ordered by their total, those whose size is not measured last.
func groupByReason(items []item, bySize bool) [][]item {
	var groups [][]item
	at := map[string]int{}
	for _, it := range items {
		i, ok := at[it.c.Reason]
		if !ok {
			i = len(groups)
			at[it.c.Reason] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], it)
	}
	if !bySize {
		return groups
	}
	for _, g := range groups {
		slices.SortStableFunc(g, func(a, b item) int { return cmp.Compare(b.c.Size, a.c.Size) })
	}
	slices.SortStableFunc(groups, func(a, b []item) int {
		if sa, sb := len(a) == 1, len(b) == 1; sa != sb {
			if sa {
				return -1
			}
			return 1
		}
		if ua, ub := allUnmeasured(a), allUnmeasured(b); ua != ub {
			if ua {
				return 1
			}
			return -1
		}
		return cmp.Compare(groupSize(b), groupSize(a))
	})
	return groups
}

// view holds what every line of one tier is rendered with.
type view struct {
	home      string
	now       time.Time
	manual    bool
	numWidth  int // stale numbering width; 0 when not numbered
	nameWidth int // column singleton reasons are aligned to
	st        style
}

// writeGroup prints a reason once, then its items. Anonymous Docker volumes
// (64-hex names) fold into one line: their names say nothing to a person.
func (v view) writeGroup(ew *errWriter, g []item) {
	var anon int
	var named []item
	for _, it := range g {
		if isVolume(it.c) && collect.IsAnonymousVolume(it.c.Path) {
			anon++
		} else {
			named = append(named, it)
		}
	}
	size := "size not measured"
	if !allUnmeasured(g) {
		size = formatSize(groupSize(g))
	}
	ew.printf("  %s %s\n", v.st.bold(g[0].c.Reason), v.st.dim(fmt.Sprintf("— %d %s, %s", len(g), items(len(g)), size)))
	sized := !allUnmeasured(g)
	v.nameWidth = 0
	for _, it := range named {
		v.nameWidth = max(v.nameWidth, min(len([]rune(v.name(it.c))), maxNameWidth))
	}
	for _, it := range named {
		if sized {
			ew.printf("%s\n", v.line("  ", it, usedBy(it.c)))
		} else {
			ew.printf("    %s%s%s\n", v.number(it), v.labeled(v.name(it.c), usedBy(it.c)), v.lastUse(it.c))
		}
	}
	if anon > 0 {
		msg := fmt.Sprintf("%d anonymous volumes (names in --json)", anon)
		if anon == 1 {
			msg = "1 anonymous volume (name in --json)"
		}
		if g[0].c.Reason == collect.ReasonUnusedVolume {
			msg += "; `docker volume prune` removes them"
		}
		ew.printf("    %s\n", v.st.dim(msg))
	}
}

// line renders one item: number, size, name, the note when given, and last
// use when known.
func (v view) line(indent string, it item, note string) string {
	size := v.st.size(it.c, fmt.Sprintf("%10s", displaySize(it.c)))
	return indent + v.number(it) + size + "  " + v.labeled(v.name(it.c), note) + v.lastUse(it.c)
}

// labeled follows name with note, aligned to the name column.
func (v view) labeled(name, note string) string {
	if note == "" {
		return name
	}
	return name + strings.Repeat(" ", max(v.nameWidth-len([]rune(name)), 0)) + "  " + v.st.dim(note)
}

// usedBy names the containers that keep a volume, at most two.
func usedBy(c classify.Candidate) string {
	switch n := len(c.UsedBy); {
	case n == 0:
		return ""
	case n <= 2:
		return "used by " + strings.Join(c.UsedBy, ", ")
	default:
		return fmt.Sprintf("used by %s +%d more", strings.Join(c.UsedBy[:2], ", "), n-2)
	}
}

func (v view) number(it item) string {
	if v.numWidth == 0 {
		return ""
	}
	return v.st.number(fmt.Sprintf("[%*d]", v.numWidth, it.n)) + " "
}

// name is what identifies an item: the command to run for manual items, else
// the path, with the home directory shortened to ~. A volume containers still
// use shows its name: docker would refuse the command.
func (v view) name(c classify.Candidate) string {
	if v.manual && c.ReclaimCmd != "" && len(c.UsedBy) == 0 {
		return tildeCommand(c.ReclaimCmd, v.home)
	}
	return tildePath(displayName(c), v.home)
}

func (v view) lastUse(c classify.Candidate) string {
	if c.LastUse.At.IsZero() {
		return ""
	}
	return v.st.dim("  last used " + formatLastUse(c.LastUse, v.now))
}

// sizeUnmeasured reports whether a zero size means "not measured" rather
// than empty.
func sizeUnmeasured(c classify.Candidate) bool { return c.SizeUnknown }

func allUnmeasured(g []item) bool {
	for _, it := range g {
		if !sizeUnmeasured(it.c) {
			return false
		}
	}
	return true
}

// tierTotal is a tier's heading: its size and item count, and how many items
// that size leaves out because they are not measured.
func tierTotal(g []item, total int64) string {
	var unmeasured int
	for _, it := range g {
		if sizeUnmeasured(it.c) {
			unmeasured++
		}
	}
	switch {
	case unmeasured == len(g):
		return fmt.Sprintf("%d %s, size not measured", len(g), items(len(g)))
	case unmeasured > 0:
		return fmt.Sprintf("%s in %d %s (%d not measured)", formatSize(total), len(g), items(len(g)), unmeasured)
	}
	return fmt.Sprintf("%s in %d %s", formatSize(total), len(g), items(len(g)))
}

func groupSize(g []item) int64 {
	var n int64
	for _, it := range g {
		n += it.c.Size
	}
	return n
}

func displaySize(c classify.Candidate) string {
	if sizeUnmeasured(c) {
		return "-"
	}
	return formatSize(c.Size)
}

// isVolume reports whether c is a Docker volume; its Path is the volume name.
func isVolume(c classify.Candidate) bool {
	return c.Category == classify.CategoryDocker && c.ReclaimCmd == "docker volume rm "+c.Path
}

// tildePath shortens a path under home to ~/....
func tildePath(p, home string) string {
	if home == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, home+"/"); ok {
		return "~/" + rest
	}
	return p
}

// tildeCommand shortens the home directory in a command's arguments; the
// shell expands an unquoted leading ~, so the command still runs as printed.
func tildeCommand(cmd, home string) string {
	if home == "" {
		return cmd
	}
	return strings.ReplaceAll(cmd, " "+home+"/", " ~/")
}

// writeTips prints each distinct tip once, in order of first appearance. A
// tip's continuation lines are indented under its bullet.
func writeTips(ew *errWriter, st style, cands []classify.Candidate) {
	seen := map[string]bool{}
	var tips []string
	for _, c := range cands {
		if c.Tip != "" && !seen[c.Tip] {
			seen[c.Tip] = true
			tips = append(tips, c.Tip)
		}
	}
	if len(tips) == 0 {
		return
	}
	ew.printf("%s\n", st.bold("Tips"))
	for _, t := range tips {
		lines := wrapText(t, tipWidth)
		ew.printf("  %s %s\n", st.wrap("•", sgrCyan), lines[0])
		for _, l := range lines[1:] {
			ew.printf("    %s\n", l)
		}
	}
	ew.printf("\n")
}

// tipWidth is where tips wrap, leaving room for their 4-column indent.
const tipWidth = 76

// wrapText breaks text into lines of at most width runes at spaces, keeping
// its own line breaks and the leading spaces of each line (indented
// snippets). A `quoted command` is never split, and a word or command longer
// than width gets a line of its own.
func wrapText(text string, width int) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(para, " ")
		line := para[:len(para)-len(trimmed)]
		indent := len(line)
		for _, w := range wrapWords(trimmed) {
			if n := len([]rune(line)); n > indent && n+1+len([]rune(w)) > width {
				out = append(out, line)
				line = strings.Repeat(" ", indent)
			}
			if len([]rune(line)) > indent {
				line += " "
			}
			line += w
		}
		out = append(out, line)
	}
	return out
}

// wrapWords splits s at spaces, keeping each `backquoted span` whole.
func wrapWords(s string) []string {
	var out []string
	open := false
	for _, f := range strings.Fields(s) {
		if open {
			out[len(out)-1] += " " + f
		} else {
			out = append(out, f)
		}
		if strings.Count(f, "`")%2 == 1 {
			open = !open
		}
	}
	return out
}

// writeTrailer writes the skipped collectors, warnings and notices that
// follow the candidates in every human-readable rendering.
func writeTrailer(ew *errWriter, r Report) {
	st := style{on: r.Color}
	if len(r.Skipped) > 0 {
		ew.printf("%s\n", st.warn("Skipped collectors"))
		for _, s := range r.Skipped {
			ew.printf("  %s: %s\n", s.Collector, s.Reason)
		}
		ew.printf("\n")
	}
	if len(r.Warnings) > 0 {
		ew.printf("%s\n", st.warn("Warnings"))
		for _, m := range r.Warnings {
			ew.printf("  %s\n", m)
		}
		ew.printf("\n")
	}
	if len(r.Notices) > 0 {
		ew.printf("%s\n", st.bold("Notices"))
		for _, m := range r.Notices {
			ew.printf("  %s\n", m)
		}
		ew.printf("\n")
	}
}

type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) printf(format string, a ...any) {
	if e.err == nil {
		_, e.err = fmt.Fprintf(e.w, format, a...)
	}
}

func displayName(c classify.Candidate) string {
	if c.Category == classify.CategoryDocker {
		if id, ok := strings.CutPrefix(c.Path, "sha256:"); ok {
			if len(id) > 12 {
				id = id[:12]
			}
			return id
		}
	}
	return c.Path
}

func formatLastUse(u classify.LastUse, now time.Time) string {
	if u.At.IsZero() {
		return "-"
	}
	s := u.At.Format("2006-01-02")
	days := int(now.Sub(u.At).Hours() / 24)
	if days < 0 {
		days = 0
	}
	s += fmt.Sprintf(" (%d days ago", days)
	if n, ok := signalNames[u.Source]; ok {
		s += ", " + n
	}
	return s + ")"
}

func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	for _, u := range []string{"KiB", "MiB", "GiB", "TiB", "PiB"} {
		f /= unit
		if f < unit || u == "PiB" {
			return fmt.Sprintf("%.1f %s", f, u)
		}
	}
	return ""
}
