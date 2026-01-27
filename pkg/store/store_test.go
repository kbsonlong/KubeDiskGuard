package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"KubeDiskGuard/pkg/profiler"
)

func TestJSONStore_SaveAndLoadFresh(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perf.json")
	s := NewJSONStore(path, 10*time.Second)
	b := profiler.Baseline{
		Profiles: map[string]profiler.PerfProfile{
			"1:0": {DeviceID: "1:0", RandReadIOPS: 100, RandWriteIOPS: 80, SeqReadBPS: 1024, SeqWriteBPS: 2048},
		},
		Version: "v1",
		TTL:     10 * time.Second,
	}
	if err := s.Save(b); err != nil {
		t.Fatalf("save error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
	loaded, fresh, err := s.Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if !fresh {
		t.Fatalf("expected fresh baseline")
	}
	if len(loaded.Profiles) != 1 {
		t.Fatalf("expected profiles=1 got=%d", len(loaded.Profiles))
	}
}

func TestJSONStore_LoadExpired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "perf.json")
	s := NewJSONStore(path, 1*time.Nanosecond)
	b := profiler.Baseline{
		Profiles: map[string]profiler.PerfProfile{},
		Version:  "v1",
		TTL:      1 * time.Nanosecond,
	}
	if err := s.Save(b); err != nil {
		t.Fatalf("save error: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	_, fresh, err := s.Load()
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if fresh {
		t.Fatalf("expected expired baseline")
	}
}
