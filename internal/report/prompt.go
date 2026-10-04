package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParseSelection parses a stale-item selection such as "all", "none" or
// "1,3-5" against n items. It returns 1-based indexes, sorted ascending and
// deduplicated. Errors name the offending token.
func ParseSelection(input string, n int) ([]int, error) {
	s := strings.ToLower(strings.TrimSpace(input))
	switch s {
	case "", "none":
		return []int{}, nil
	case "all":
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out, nil
	}

	seen := map[int]bool{}
	for _, raw := range strings.Split(s, ",") {
		tok := strings.TrimSpace(raw)
		if tok == "" {
			return nil, fmt.Errorf("empty token in selection %q", input)
		}
		lo, hi, err := parseToken(tok)
		if err != nil {
			return nil, err
		}
		if lo < 1 || hi > n {
			return nil, fmt.Errorf("%q is out of range (1-%d)", tok, n)
		}
		if lo > hi {
			return nil, fmt.Errorf("%q is a reversed range", tok)
		}
		for i := lo; i <= hi; i++ {
			seen[i] = true
		}
	}
	out := make([]int, 0, len(seen))
	for i := range seen {
		out = append(out, i)
	}
	sort.Ints(out)
	return out, nil
}

// parseToken parses "N" or "A-B" into an inclusive range.
func parseToken(tok string) (int, int, error) {
	a, b, isRange := strings.Cut(tok, "-")
	if !isRange {
		v, err := strconv.Atoi(tok)
		if err != nil {
			return 0, 0, fmt.Errorf("%q is not a number or range", tok)
		}
		return v, v, nil
	}
	lo, errLo := strconv.Atoi(strings.TrimSpace(a))
	hi, errHi := strconv.Atoi(strings.TrimSpace(b))
	if errLo != nil || errHi != nil {
		return 0, 0, fmt.Errorf("%q is not a number or range", tok)
	}
	return lo, hi, nil
}
