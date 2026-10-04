package history

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/juanmhidalgo/devclean/internal/testenv"
)

func TestSaveConcurrentWriters(t *testing.T) {
	state, _ := testenv.Isolate(t)
	path := filepath.Join(state, "devclean", "history.json")
	now := func() time.Time { return time.Unix(1000, 0) }
	t0 := time.Unix(100, 0).UTC()

	const writers = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, _, err := Load(path, now)
			if err != nil {
				errs <- err
				return
			}
			h.Entries[fmt.Sprintf("k%d", i)] = Entry{FirstSeen: t0, LastUsed: t0}
			_, err = Save(path, h, now)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := Load(path, now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < writers; i++ {
		if _, ok := got.Entries[fmt.Sprintf("k%d", i)]; !ok {
			t.Errorf("entry k%d lost; have %v", i, got.Entries)
		}
	}
}

func TestSaveMergeSemantics(t *testing.T) {
	state, _ := testenv.Isolate(t)
	path := filepath.Join(state, "history.json")
	now := func() time.Time { return time.Unix(1000, 0) }
	at := func(s int64) time.Time { return time.Unix(s, 0).UTC() }

	disk := empty()
	disk.Entries["a"] = Entry{FirstSeen: at(50), LastUsed: at(200)}
	disk.Walked["go"] = at(300)
	if _, err := Save(path, disk, now); err != nil {
		t.Fatal(err)
	}
	mem := empty()
	mem.Entries["a"] = Entry{FirstSeen: at(10), LastUsed: at(100)}
	mem.Entries["z"] = Entry{LastUsed: at(5)}
	mem.Walked["go"] = at(100)
	mem.Walked["npm"] = at(400)
	if _, err := Save(path, mem, now); err != nil {
		t.Fatal(err)
	}
	got, _, _ := Load(path, now)
	if e := got.Entries["a"]; !e.FirstSeen.Equal(at(10)) || !e.LastUsed.Equal(at(200)) {
		t.Errorf("a = %+v, want first 10 last 200", e)
	}
	if !got.Entries["z"].LastUsed.Equal(at(5)) {
		t.Errorf("z lost: %+v", got.Entries["z"])
	}
	if !got.Walked["go"].Equal(at(300)) || !got.Walked["npm"].Equal(at(400)) {
		t.Errorf("walked = %v", got.Walked)
	}
}
