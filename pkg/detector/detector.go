package detector

import (
	"os"
	"os/exec"
)

// DetectRuntime 检测容器运行时
func DetectRuntime() string {
	if _, err := exec.LookPath("docker"); err == nil {
		return "docker"
	}
	if _, err := exec.LookPath("ctr"); err == nil {
		return "containerd"
	}
	return "none"
}

// DetectCgroupVersion 检测cgroup版本
func DetectCgroupVersion() string {
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
		return "v2"
	}
	return "v1"
}
