package profiler

import "time"

type PerfProfile struct {
	DeviceID       string    `json:"device_id"`
	DeviceName     string    `json:"device_name"`
	MountPoint     string    `json:"mount_point"`
	RandReadIOPS   int       `json:"rand_read_iops"`
	RandWriteIOPS  int       `json:"rand_write_iops"`
	SeqReadBPS     int       `json:"seq_read_bps"`
	SeqWriteBPS    int       `json:"seq_write_bps"`
	Method         string    `json:"method"`
	Samples        int       `json:"samples"`
	SampleDuration int       `json:"sample_duration"`
	Timestamp      time.Time `json:"timestamp"`
}

type Baseline struct {
	Profiles map[string]PerfProfile `json:"profiles"`
	Version  string                 `json:"version"`
	TTL      time.Duration          `json:"ttl"`
}
