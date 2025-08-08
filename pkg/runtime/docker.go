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

// getCgroupPath 通过 Docker API 获取容器的 cgroup 路径
func (d *DockerRuntime) getCgroupPath(containerID string) (string, error) {
	ctx := context.Background()
	
	// 获取容器详细信息
	info, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %v", err)
	}
	
	// 从容器信息中获取 cgroup 路径
	// Docker 容器的 cgroup 路径通常在 HostConfig.CgroupParent 或者可以从容器 ID 构建
	var cgroupsPath string
	if info.HostConfig.CgroupParent != "" {
		// 如果有明确的 cgroup parent，使用它
		cgroupsPath = fmt.Sprintf("%s/docker-%s.scope", info.HostConfig.CgroupParent, containerID)
	} else {
		// 默认的 Docker cgroup 路径格式
		if d.config.CgroupVersion == "v1" {
			cgroupsPath = fmt.Sprintf("/docker/%s", containerID)
		} else {
			// cgroup v2 的路径格式
			cgroupsPath = fmt.Sprintf("/system.slice/docker-%s.scope", containerID)
		}
	}
	
	// 根据 cgroup 版本构建完整路径
	if d.config.CgroupVersion == "v1" {
		// cgroup v1: 需要指定子系统路径
		return fmt.Sprintf("/sys/fs/cgroup/blkio%s", cgroupsPath), nil
	} else {
		// cgroup v2: 统一层次结构
		return fmt.Sprintf("/sys/fs/cgroup%s", cgroupsPath), nil
	}
}

// SetLimits 统一设置IOPS和BPS限制
func (d *DockerRuntime) SetLimits(container *container.ContainerInfo, riops, wiops, rbps, wbps int) error {
	majMin, err := device.GetMajMin(d.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath, err := d.getCgroupPath(container.ID)
	if err != nil {
		return fmt.Errorf("failed to get cgroup path for container %s: %v", container.ID, err)
	}
	return d.cgroup.SetLimits(cgroupPath, majMin, riops, wiops, rbps, wbps)
}

// ResetLimits 统一解除所有限速
func (d *DockerRuntime) ResetLimits(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(d.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath, err := d.getCgroupPath(container.ID)
	if err != nil {
		return fmt.Errorf("failed to get cgroup path for container %s: %v", container.ID, err)
	}
	return d.cgroup.ResetLimits(cgroupPath, majMin)
}
