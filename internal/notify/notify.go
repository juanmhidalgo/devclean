// Package notify delivers the user's notify_command notification.
package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Send runs cmd with "sh -c" and writes body to its stdin. The body is never
// part of argv, so a leading "-" or shell metacharacters in it are inert.
// An empty cmd means notifications are not configured and Send returns nil.
// A failing command yields an error "<cmd>: <err>: <output|no output>".
func Send(ctx context.Context, cmd, body string) error {
	if cmd == "" {
		return nil
	}
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Stdin = strings.NewReader(body)
	out, err := c.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			text = "no output"
		}
		return fmt.Errorf("%s: %w: %s", cmd, err, text)
	}
	return nil
}
