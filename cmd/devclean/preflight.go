package main

import (
	"errors"
	"fmt"

	"github.com/juanmhidalgo/devclean/internal/platform"
)

// preflight is the first runtime gate: it returns a non-zero exit code, after
// printing why, when devclean must not run. It never invokes a collector.
func (a *app) preflight() int {
	if err := a.platform.Supported(); err != nil {
		if errors.Is(err, platform.ErrUnsupported) {
			fmt.Fprintln(a.stderr, "devclean: unsupported platform")
		} else {
			fmt.Fprintln(a.stderr, "devclean: unsupported platform:", err)
		}
		return 1
	}
	if a.platform.Euid() == 0 {
		fmt.Fprintln(a.stderr, "devclean: refusing to run as root")
		return 1
	}
	return 0
}
