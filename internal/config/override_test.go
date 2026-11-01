package config

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
)

func TestOverrideValidationAndIOErrors(t *testing.T) {
	home := profileHome(t)
	const good = `{"mode":"rule"}`
	for _, slot := range []OverrideSlot{OverrideSlotPersist, OverrideSlotSession} {
		if err := WriteOverride(slot, good); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`{`, `[]`, `null`, `"text"`} {
			if err := WriteOverride(slot, bad); err == nil {
				t.Fatalf("accepted %q", bad)
			}
			if got := ReadOverride(slot); got != good {
				t.Fatalf("override changed to %q", got)
			}
		}
		if err := ClearOverride(slot); err != nil {
			t.Fatal(err)
		}
		if got := ReadOverride(slot); got != "{}" {
			t.Fatalf("clear returned %q", got)
		}
	}
	if err := WriteOverride(99, good); err == nil {
		t.Fatal("accepted invalid slot")
	}
	// A directory at the destination makes replacement fail even when run as root.
	if err := os.Mkdir(filepath.Join(home, "override.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteOverride(OverrideSlotPersist, good); err == nil {
		t.Fatal("lost I/O error")
	}
}

func TestOverrideClosesFilesWithoutGC(t *testing.T) {
	profileHome(t)
	fdPath := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		fdPath = "/dev/fd"
	}
	before, err := os.ReadDir(fdPath)
	if err != nil {
		t.Skipf("cannot count descriptors: %v", err)
	}
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	for i := 0; i < 100; i++ {
		if err := WriteOverride(OverrideSlotPersist, "{}"); err != nil {
			t.Fatal(err)
		}
		if got := ReadOverride(OverrideSlotPersist); got != "{}" {
			t.Fatalf("read %q", got)
		}
	}
	after, err := os.ReadDir(fdPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before)+2 {
		t.Fatalf("file descriptors grew from %d to %d", len(before), len(after))
	}
}

func TestSessionOverrideConcurrentAccess(t *testing.T) {
	defer ClearOverride(OverrideSlotSession)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if err := WriteOverride(OverrideSlotSession, "{}"); err != nil {
					t.Error(err)
				}
				ReadOverride(OverrideSlotSession)
				ClearOverride(OverrideSlotSession)
			}
		}()
	}
	wg.Wait()
}
