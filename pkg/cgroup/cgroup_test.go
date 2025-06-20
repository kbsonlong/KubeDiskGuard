package cgroup

import (
	"os"
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager("v1")
	if m == nil || m.version != "v1" {
		t.Errorf("NewManager failed")
	}
}

func TestFindCgroupPath(t *testing.T) {
	m := NewManager("v1")
	_ = m.FindCgroupPath("fakeid") // 只要不panic即可
	m = NewManager("v2")
	_ = m.FindCgroupPath("fakeid")
}

func TestBuildCgroupPath(t *testing.T) {
	m := NewManager("v1")
	p := m.BuildCgroupPath("cid", "/parent")
	if p == "" {
		t.Errorf("BuildCgroupPath failed")
	}
	m = NewManager("v2")
	_ = m.BuildCgroupPath("cid", "/parent")
}

func TestSetAndResetIOPSLimit(t *testing.T) {
	m := NewManager("v1")
	dir := t.TempDir()
	readFile := dir + "/blkio.throttle.read_iops_device"
	writeFile := dir + "/blkio.throttle.write_iops_device"
	os.WriteFile(readFile, []byte{}, 0644)
	os.WriteFile(writeFile, []byte{}, 0644)
	err := m.SetIOPSLimit(dir, "8:0", 100)
	if err != nil {
		t.Errorf("SetIOPSLimit failed: %v", err)
	}
	err = m.ResetIOPSLimit(dir, "8:0")
	if err != nil {
		t.Errorf("ResetIOPSLimit failed: %v", err)
	}
}

func TestSetAndResetBPSLimit(t *testing.T) {
	m := NewManager("v1")
	dir := t.TempDir()
	readFile := dir + "/blkio.throttle.read_bps_device"
	writeFile := dir + "/blkio.throttle.write_bps_device"
	os.WriteFile(readFile, []byte{}, 0644)
	os.WriteFile(writeFile, []byte{}, 0644)
	err := m.SetBPSLimit(dir, "8:0", 1024, 2048)
	if err != nil {
		t.Errorf("SetBPSLimit failed: %v", err)
	}
	err = m.ResetBPSLimit(dir, "8:0")
	if err != nil {
		t.Errorf("ResetBPSLimit failed: %v", err)
	}
}

func TestSetAndResetLimits(t *testing.T) {
	m := NewManager("v1")
	dir := t.TempDir()
	for _, f := range []string{"blkio.throttle.read_iops_device", "blkio.throttle.write_iops_device", "blkio.throttle.read_bps_device", "blkio.throttle.write_bps_device"} {
		os.WriteFile(dir+"/"+f, []byte{}, 0644)
	}
	err := m.SetLimits(dir, "8:0", 100, 200, 1024, 2048)
	if err != nil {
		t.Errorf("SetLimits failed: %v", err)
	}
	err = m.SetLimits(dir, "8:0", 0, 0, 0, 0) // 应该清空所有
	if err != nil {
		t.Errorf("SetLimits reset failed: %v", err)
	}
	err = m.ResetLimits(dir, "8:0")
	if err != nil {
		t.Errorf("ResetLimits failed: %v", err)
	}
}
