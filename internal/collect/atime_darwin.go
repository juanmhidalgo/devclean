package collect

import (
	"os"
	"syscall"
	"time"
)

// atimeOf returns the access time from an Lstat result.
func atimeOf(fi os.FileInfo) (time.Time, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(st.Atimespec.Sec, st.Atimespec.Nsec), true
}
