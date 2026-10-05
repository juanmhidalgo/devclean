package report

import "github.com/juanmhidalgo/devclean/internal/classify"

// style adds ANSI SGR escapes when on, and is a no-op otherwise. Every helper
// takes text already padded to its column, so escapes never break alignment.
type style struct{ on bool }

const (
	sgrBold    = "1"
	sgrDim     = "2"
	sgrRed     = "31"
	sgrGreen   = "32"
	sgrYellow  = "33"
	sgrBlue    = "34"
	sgrMagenta = "35"
	sgrCyan    = "36"
)

func (s style) wrap(text string, codes ...string) string {
	if !s.on || text == "" || len(codes) == 0 {
		return text
	}
	seq := "\x1b["
	for i, c := range codes {
		if i > 0 {
			seq += ";"
		}
		seq += c
	}
	return seq + "m" + text + "\x1b[0m"
}

func (s style) bold(text string) string { return s.wrap(text, sgrBold) }
func (s style) dim(text string) string  { return s.wrap(text, sgrDim) }
func (s style) warn(text string) string { return s.wrap(text, sgrBold, sgrYellow) }

// tierColor tells the tiers apart: green is deleted without asking, cyan
// only under disk pressure, yellow asks first, magenta is left to the user.
var tierColor = map[classify.Tier]string{
	classify.TierGarbage: sgrGreen,
	classify.TierCaches:  sgrCyan,
	classify.TierStale:   sgrYellow,
	classify.TierManual:  sgrMagenta,
}

func (s style) tier(t classify.Tier, text string) string {
	return s.wrap(text, sgrBold, tierColor[t])
}

// size highlights what is worth a look: 1 GiB and up stands out, under
// 1 MiB and unmeasured sizes fade.
func (s style) size(c classify.Candidate, padded string) string {
	switch {
	case c.SizeUnknown || c.Size < 1<<20:
		return s.dim(padded)
	case c.Size >= 1<<30:
		return s.wrap(padded, sgrBold, sgrRed)
	}
	return padded
}

// number is a stale item's selection number, the thing the prompt asks for.
func (s style) number(text string) string { return s.wrap(text, sgrBold, sgrBlue) }
