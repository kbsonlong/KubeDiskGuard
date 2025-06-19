package detector

import (
	"context"
	"os"
	"os/exec"

	"github.com/containerd/containerd"
	"github.com/docker/docker/client"
)

// DetectRuntime 检测容器运行时
func DetectRuntime() string {
	socket := os.Getenv("CONTAINER_SOCKET_PATH")
	if socket == "" {
		socket = "/run/containerd/containerd.sock"
	}

	// 先尝试docker SDK
	if cli, err := client.NewClientWithOpts(client.WithHost("unix://" + socket)); err == nil {
		defer cli.Close()
		if _, err := cli.Ping(context.Background()); err == nil {
			return "docker"
		}
	}

	// 再尝试containerd SDK
	if c, err := containerd.New(socket); err == nil {
		defer c.Close()
		if _, err := c.Version(context.Background()); err == nil {
			return "containerd"
		}
	}

	// fallback到LookPath
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
