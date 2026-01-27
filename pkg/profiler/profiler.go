package profiler

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"KubeDiskGuard/pkg/device"
)

type Config struct {
	Enabled        bool
	TTL            time.Duration
	StorePath      string
	MaxLoadAvg     float64
	MaxSampleSecs  int
	MaxFileMB      int
	MountPoint     string
}

type Guard interface {
	Allow() bool
}

type simpleGuard struct {
	maxLoad float64
}

func (g *simpleGuard) Allow() bool {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return true
	}
	var load float64
	_, _ = fmt.Sscanf(string(data), "%f", &load)
	return load <= g.maxLoad
}

type Manager struct {
	cfg   Config
	guard Guard
}

func NewManager(cfg Config) *Manager {
	return &Manager{
		cfg:   cfg,
		guard: &simpleGuard{maxLoad: cfg.MaxLoadAvg},
	}
}

func (m *Manager) Sample() (Baseline, error) {
	if !m.cfg.Enabled {
		return Baseline{Profiles: map[string]PerfProfile{}}, nil
	}
	if !m.guard.Allow() {
		return Baseline{Profiles: map[string]PerfProfile{}}, fmt.Errorf("high load, skip sampling")
	}
	majmin, err := device.GetMajMin(m.cfg.MountPoint)
	if err != nil {
		return Baseline{}, fmt.Errorf("resolve device: %w", err)
	}
	profile, err := m.sampleOnMount(m.cfg.MountPoint, majmin)
	if err != nil {
		return Baseline{}, err
	}
	return Baseline{
		Profiles: map[string]PerfProfile{majmin: profile},
		Version:  "v1",
		TTL:      m.cfg.TTL,
	}, nil
}

func (m *Manager) sampleOnMount(mount, deviceID string) (PerfProfile, error) {
	size := m.cfg.MaxFileMB * 1024 * 1024
	if size <= 0 {
		size = 32 * 1024 * 1024
	}
	tmpDir := filepath.Join(mount, ".kubediskguard")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return PerfProfile{}, err
	}
	tmpFile := filepath.Join(tmpDir, "perf.tmp")
	f, err := os.Create(tmpFile)
	if err != nil {
		return PerfProfile{}, err
	}
	defer func() {
		f.Close()
		os.Remove(tmpFile)
	}()
	if err := preallocate(f, size); err != nil {
		return PerfProfile{}, err
	}
	riops := measureRandIOPS(f, size, m.cfg.MaxSampleSecs)
	wiops := measureRandWriteIOPS(f, size, m.cfg.MaxSampleSecs)
	rbps := measureSeqBPS(f, size, m.cfg.MaxSampleSecs, true)
	wbps := measureSeqBPS(f, size, m.cfg.MaxSampleSecs, false)
	return PerfProfile{
		DeviceID:       deviceID,
		DeviceName:     "",
		MountPoint:     mount,
		RandReadIOPS:   riops,
		RandWriteIOPS:  wiops,
		SeqReadBPS:     rbps,
		SeqWriteBPS:    wbps,
		Method:         "posix",
		Samples:        1,
		SampleDuration: m.cfg.MaxSampleSecs,
		Timestamp:      time.Now(),
	}, nil
}

func preallocate(f *os.File, size int) error {
	buf := make([]byte, 1024*1024)
	for written := 0; written < size; written += len(buf) {
		if _, err := f.Write(buf); err != nil {
			return err
		}
	}
	return f.Sync()
}

func measureRandIOPS(f *os.File, size int, secs int) int {
	block := 4096
	count := 0
	start := time.Now()
	r := rand.New(rand.NewSource(start.UnixNano()))
	buf := make([]byte, block)
	for time.Since(start) < time.Duration(secs)*time.Second {
		offset := (r.Intn(size/block-1)) * block
		if _, err := f.ReadAt(buf, int64(offset)); err == nil || err == io.EOF {
			count++
		}
	}
	return count / secs
}

func measureRandWriteIOPS(f *os.File, size int, secs int) int {
	block := 4096
	count := 0
	start := time.Now()
	r := rand.New(rand.NewSource(start.UnixNano()))
	buf := make([]byte, block)
	for time.Since(start) < time.Duration(secs)*time.Second {
		offset := (r.Intn(size/block-1)) * block
		if _, err := f.WriteAt(buf, int64(offset)); err == nil {
			count++
		}
	}
	_ = f.Sync()
	return count / secs
}

func measureSeqBPS(f *os.File, size int, secs int, read bool) int {
	block := 128 * 1024
	buf := make([]byte, block)
	total := 0
	start := time.Now()
	for time.Since(start) < time.Duration(secs)*time.Second {
		if read {
			if _, err := f.Read(buf); err != nil && err != io.EOF {
				break
			}
		} else {
			if _, err := f.Write(buf); err != nil {
				break
			}
		}
		total += block
	}
	if !read {
		_ = f.Sync()
	}
	elapsed := int(time.Since(start).Seconds())
	if elapsed == 0 {
		return 0
	}
	return total / elapsed
}
