package platform

import (
	"strings"
	"testing"
)

const mountinfoFixture = `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
30 22 8:2 / /home rw,noatime shared:2 - ext4 /dev/sda2 rw
31 30 8:3 / /home/me/my\040projects rw,noatime,nodiratime - ext4 /dev/sda3 rw
32 22 8:4 / /home2 ro,relatime - ext4 /dev/sda4 rw
`

func TestParseMountinfo(t *testing.T) {
	mounts, err := ParseMountinfo(strings.NewReader(mountinfoFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 4 {
		t.Fatalf("got %d mounts, want 4", len(mounts))
	}
	if mounts[2].Point != "/home/me/my projects" {
		t.Errorf("escaped path = %q", mounts[2].Point)
	}

	tests := []struct {
		path        string
		wantPoint   string
		wantNoAtime bool
	}{
		{"/etc/x", "/", false},                                     // relatime is not noatime
		{"/home/me/code", "/home", true},                           // noatime
		{"/home/me/my projects/a/b", "/home/me/my projects", true}, // longest prefix, escaped
		{"/home2/x", "/home2", false},                              // prefix must end at a path boundary
		{"/home", "/home", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			m, ok := FindMount(mounts, tt.path)
			if !ok {
				t.Fatal("no mount found")
			}
			if m.Point != tt.wantPoint || m.NoAtime != tt.wantNoAtime {
				t.Errorf("got {%q noatime=%v}, want {%q noatime=%v}", m.Point, m.NoAtime, tt.wantPoint, tt.wantNoAtime)
			}
		})
	}
}
