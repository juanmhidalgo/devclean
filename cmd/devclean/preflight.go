package main

import (
	"errors"

	"github.com/juanmhidalgo/devclean/internal/platform"
)

// preflight is the first runtime gate: it returns a non-zero exit code, after
// printing why, when devclean must not run. It never invokes a collector.
func (a *app) preflight() int {
	if err := a.platform.Supported(); err != nil {
		if errors.Is(err, platform.ErrUnsupported) {
			a.stderrf("devclean: unsupported platform\n")
		} else {
			a.stderrf("devclean: unsupported platform: %v\n", err)
		}
		return 1
	}
	if a.platform.Euid() == 0 {
		a.stderrf("devclean: refusing to run as root\n")
		return 1
	}
	return 0
}
