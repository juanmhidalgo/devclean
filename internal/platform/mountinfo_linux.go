package platform

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseMountinfo parses the kernel mountinfo format (one mount per line).
// Octal escapes such as \040 in the mount point are decoded.
func ParseMountinfo(r io.Reader) ([]Mount, error) {
	var mounts []Mount
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 6 {
			return nil, fmt.Errorf("malformed mountinfo line: %q", line)
		}
		opts := strings.Split(f[5], ",")
		m := Mount{Point: unescapeOctal(f[4]), Device: f[2], Options: opts}
		for _, o := range opts {
			if o == "noatime" {
				m.NoAtime = true
			}
		}
		mounts = append(mounts, m)
	}
	return mounts, sc.Err()
}

func unescapeOctal(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
