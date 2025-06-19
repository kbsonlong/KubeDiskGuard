package runtime

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"

	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/device"
)

// ContainerdRuntime containerd运行时
type ContainerdRuntime struct {
	config *config.Config
	cgroup *cgroup.Manager
	client *containerd.Client
}

// NewContainerdRuntime 创建containerd运行时
func NewContainerdRuntime(config *config.Config) (*ContainerdRuntime, error) {
	client, err := containerd.New(config.ContainerSocketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to containerd: %v", err)
	}

	return &ContainerdRuntime{
		config: config,
		cgroup: cgroup.NewManager(config.CgroupVersion),
		client: client,
	}, nil
}

// Close 关闭containerd客户端连接
func (c *ContainerdRuntime) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// GetContainerByID 根据ID获取容器信息
func (c *ContainerdRuntime) GetContainerByID(containerID string) (*container.ContainerInfo, error) {
	ctx := namespaces.WithNamespace(context.Background(), c.config.ContainerdNamespace)

	cont, err := c.client.LoadContainer(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to load container %s: %v", containerID, err)
	}

	return c.getContainerInfo(ctx, cont)
}

// getContainerInfo 从containerd容器对象获取容器信息
func (c *ContainerdRuntime) getContainerInfo(ctx context.Context, cont containerd.Container) (*container.ContainerInfo, error) {
	info, err := cont.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container info: %v", err)
	}

	containerInfo := &container.ContainerInfo{
		ID:          cont.ID(),
		Name:        info.Labels["io.kubernetes.container.name"],
		Annotations: map[string]string{},
	}

	// 获取镜像信息
	if info.Image != "" {
		containerInfo.Image = info.Image
	} else {
		// 尝试从标签获取镜像信息
		if imageName, exists := info.Labels["io.kubernetes.container.image"]; exists {
			containerInfo.Image = imageName
		}
	}

	// 获取注解信息
	for k, v := range info.Labels {
		if strings.HasPrefix(k, "annotation.") {
			containerInfo.Annotations[k] = v
		}
	}

	return containerInfo, nil
}

// ProcessContainer 处理容器
func (c *ContainerdRuntime) ProcessContainer(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(c.config.DataMount)
	if err != nil {
		log.Printf("Failed to get major:minor for container %s: %v", container.ID, err)
		return err
	}

	cgroupPath := c.cgroup.FindCgroupPath(container.ID)
	log.Printf("Found cgroup path for container %s: %s", container.Name, cgroupPath)
	if err := c.cgroup.SetIOPSLimit(cgroupPath, majMin, c.config.ContainerIOPSLimit); err != nil {
		log.Printf("Failed to set IOPS limit for container %s: %v", container.ID, err)
		return err
	}

	log.Printf("Successfully set IOPS limit for container %s: %s %d", container.Name, majMin, c.config.ContainerIOPSLimit)
	return nil
}

// SetLimits 统一设置IOPS和BPS限制
func (c *ContainerdRuntime) SetLimits(container *container.ContainerInfo, riops, wiops, rbps, wbps int) error {
	majMin, err := device.GetMajMin(c.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath := c.cgroup.FindCgroupPath(container.ID)
	return c.cgroup.SetLimits(cgroupPath, majMin, riops, wiops, rbps, wbps)
}

// ResetLimits 统一解除所有限速
func (c *ContainerdRuntime) ResetLimits(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(c.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath := c.cgroup.FindCgroupPath(container.ID)
	return c.cgroup.ResetLimits(cgroupPath, majMin)
}
