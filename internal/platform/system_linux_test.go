//go:build linux

package platform

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/juanmhidalgo/devclean/internal/testenv"
)

const fakeSnapList = `cat <<'OUT'
Name    Version  Rev   Tracking       Publisher  Notes
core20  1.0      1974  latest/stable  canonical  base
core20  1.0      1891  latest/stable  canonical  base,disabled
firefox 120.0    3836  latest/stable  mozilla    disabled
OUT`

func TestLinuxSystemItems(t *testing.T) {
	t.Run("disabled snaps and journal usage (AC-11)", func(t *testing.T) {
		testenv.FakeBin(t, "snap", fakeSnapList)
		testenv.FakeBin(t, "journalctl", `echo "Archived and active journals take up 1.5G in the file systems."`)
		items, skips := Linux{}.SystemItems(context.Background())
		if len(skips) != 0 {
			t.Fatalf("skips = %v", skips)
		}
		var cmds []string
		for _, it := range items {
			cmds = append(cmds, it.ReclaimCmd)
			if it.Name == "" || it.Reason == "" {
				t.Errorf("item %+v lacks name or reason", it)
			}
		}
		want := []string{
			"sudo snap remove core20 --revision=1891",
			"sudo snap remove firefox --revision=3836",
			"sudo journalctl --vacuum-size=500M",
		}
		if !reflect.DeepEqual(cmds, want) {
			t.Errorf("reclaim cmds = %q, want %q", cmds, want)
		}
		if got := items[2].Size; got != 1610612736 {
			t.Errorf("journal size = %d, want 1610612736", got)
		}
		// One old revision per snap is already refresh.retain's minimum.
		if items[0].Tip != "" || items[1].Tip != "" {
			t.Errorf("snap tips = %q, %q, want none", items[0].Tip, items[1].Tip)
		}
		if !strings.Contains(items[2].Tip, "SystemMaxUse=500M") {
			t.Errorf("journal tip = %q", items[2].Tip)
		}
	})

	t.Run("two old revisions of a snap suggest refresh.retain", func(t *testing.T) {
		testenv.FakeBin(t, "snap", `cat <<'OUT'
Name    Version  Rev   Tracking       Publisher  Notes
core20  1.0      1974  latest/stable  canonical  base
core20  1.0      1891  latest/stable  canonical  base,disabled
core20  1.0      1800  latest/stable  canonical  base,disabled
firefox 120.0    3836  latest/stable  mozilla    disabled
OUT`)
		testenv.FakeBin(t, "journalctl", `echo "Archived and active journals take up 8.0M in the file systems."`)
		items, _ := Linux{}.SystemItems(context.Background())
		if len(items) != 3 {
			t.Fatalf("items = %+v", items)
		}
		for _, it := range items {
			if !strings.Contains(it.Tip, "refresh.retain=2") {
				t.Errorf("%s tip = %q", it.Name, it.Tip)
			}
		}
	})

	t.Run("journal under target yields nothing", func(t *testing.T) {
		testenv.FakeBin(t, "snap", "echo 'Name Version Rev Tracking Publisher Notes'")
		testenv.FakeBin(t, "journalctl", `echo "Archived and active journals take up 8.0M in the file systems."`)
		items, skips := Linux{}.SystemItems(context.Background())
		if len(items) != 0 || len(skips) != 0 {
			t.Errorf("items=%v skips=%v, want none", items, skips)
		}
	})

	t.Run("missing snap is skipped with a reason", func(t *testing.T) {
		dir := testenv.FakeBin(t, "journalctl", `echo "Archived and active journals take up 1.0G in the file systems."`)
		t.Setenv("PATH", dir)
		items, skips := Linux{}.SystemItems(context.Background())
		if len(items) != 1 || !strings.Contains(items[0].ReclaimCmd, "vacuum-size") {
			t.Errorf("items = %+v, want the journal item", items)
		}
		if len(skips) != 1 || !strings.Contains(skips[0], "snap") {
			t.Errorf("skips = %v, want one snap skip", skips)
		}
	})

	t.Run("failing journalctl is skipped", func(t *testing.T) {
		testenv.FakeBin(t, "snap", "echo 'Name Version Rev Tracking Publisher Notes'")
		testenv.FakeBin(t, "journalctl", "echo boom; exit 3")
		_, skips := Linux{}.SystemItems(context.Background())
		if len(skips) != 1 || !strings.Contains(skips[0], "exit status 3") {
			t.Errorf("skips = %v", skips)
		}
	})
}
