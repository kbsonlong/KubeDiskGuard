package runtime

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/client"

	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/device"
)

// DockerRuntime Docker运行时
type DockerRuntime struct {
	client *client.Client
	config *config.Config
	cgroup *cgroup.Manager
}

// NewDockerRuntime 创建Docker运行时
func NewDockerRuntime(config *config.Config) (*DockerRuntime, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+config.ContainerSocketPath),
		client.WithVersion("1.41"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %v", err)
	}

	return &DockerRuntime{
		client: cli,
		config: config,
		cgroup: cgroup.NewManager(config.CgroupVersion),
	}, nil
}

// GetContainerByID 根据ID获取容器信息
func (d *DockerRuntime) GetContainerByID(containerID string) (*container.ContainerInfo, error) {
	info, err := d.client.ContainerInspect(context.Background(), containerID)
	if err != nil {
		return nil, err
	}

	ci := &container.ContainerInfo{
		ID:           info.ID,
		Image:        info.Config.Image,
		Name:         strings.TrimPrefix(info.Name, "/"),
		CgroupParent: info.HostConfig.CgroupParent,
		Annotations:  map[string]string{},
	}
	for k, v := range info.Config.Labels {
		ci.Annotations[k] = v
	}
	return ci, nil
}

// Close 关闭Docker客户端连接
func (d *DockerRuntime) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// SetLimits 统一设置IOPS和BPS限制
func (d *DockerRuntime) SetLimits(container *container.ContainerInfo, riops, wiops, rbps, wbps int) error {
	majMin, err := device.GetMajMin(d.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath := d.cgroup.BuildCgroupPath(container.ID, container.CgroupParent)
	return d.cgroup.SetLimits(cgroupPath, majMin, riops, wiops, rbps, wbps)
}

// ResetLimits 统一解除所有限速
func (d *DockerRuntime) ResetLimits(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(d.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath := d.cgroup.BuildCgroupPath(container.ID, container.CgroupParent)
	return d.cgroup.ResetLimits(cgroupPath, majMin)
}
