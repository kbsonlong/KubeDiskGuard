package store

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"KubeDiskGuard/pkg/profiler"
)

type JSONStore struct {
	Path string
	TTL  time.Duration
}

type fileData struct {
	Baseline profiler.Baseline `json:"baseline"`
	Updated  time.Time         `json:"updated"`
}

func NewJSONStore(path string, ttl time.Duration) *JSONStore {
	return &JSONStore{Path: path, TTL: ttl}
}

func (s *JSONStore) Load() (profiler.Baseline, bool, error) {
	var empty profiler.Baseline
	f, err := os.Open(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, false, nil
		}
		return empty, false, err
	}
	defer f.Close()
	var fd fileData
	if err := json.NewDecoder(f).Decode(&fd); err != nil {
		return empty, false, err
	}
	if fd.Updated.IsZero() || time.Since(fd.Updated) > s.TTL {
		return fd.Baseline, false, nil
	}
	return fd.Baseline, true, nil
}

func (s *JSONStore) Save(b profiler.Baseline) error {
	tmp := s.Path + ".tmp"
	if err := os.MkdirAll(dirOf(s.Path), 0755); err != nil {
		return err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	fd := fileData{Baseline: b, Updated: time.Now()}
	if err := json.NewEncoder(f).Encode(fd); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
