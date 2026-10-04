package remove

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// RemoveImage runs `docker image rm`. A tagged image is removed by all of its
// refs: the daemon refuses `rm <id>` for an image referenced by several
// repositories unless forced, and untagging every ref deletes the image (a
// tag added since the scan keeps it). An untagged image goes by id. It never
// passes -f/--force: an image still used by a container stays, and the
// returned error carries the daemon's message so the user sees why. ctx
// bounds the docker process.
func RemoveImage(ctx context.Context, id string, refs []string) error {
	args := append([]string{"image", "rm"}, refs...)
	if len(refs) == 0 {
		args = append(args, id)
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	return nil
}
