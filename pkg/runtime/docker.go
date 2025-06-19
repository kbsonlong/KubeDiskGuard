package runtime

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	"iops-limit-service/pkg/cgroup"
	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/device"
)

// DockerRuntime Docker运行时
type DockerRuntime struct {
	client *client.Client
	config *config.Config
	cgroup *cgroup.Manager
}

// NewDockerRuntime 创建Docker运行时
func NewDockerRuntime(config *config.Config) (*DockerRuntime, error) {
	cli, err := client.NewClientWithOpts(client.WithHost("unix://" + config.ContainerSocketPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %v", err)
	}

	return &DockerRuntime{
		client: cli,
		config: config,
		cgroup: cgroup.NewManager(config.CgroupVersion),
	}, nil
}

// GetContainers 获取所有容器
func (d *DockerRuntime) GetContainers() ([]*container.ContainerInfo, error) {
	containers, err := d.client.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %v", err)
	}

	var containerInfos []*container.ContainerInfo
	for _, c := range containers {
		info, err := d.client.ContainerInspect(context.Background(), c.ID)
		if err != nil {
			log.Printf("Failed to inspect container %s: %v", c.ID, err)
			continue
		}

		ci := &container.ContainerInfo{
			ID:           info.ID,
			Image:        info.Config.Image,
			Name:         strings.TrimPrefix(info.Name, "/"),
			CgroupParent: info.HostConfig.CgroupParent,
			Annotations:  map[string]string{},
		}
		// 提取Labels到Annotations
		for k, v := range info.Config.Labels {
			ci.Annotations[k] = v
		}

		// 过滤已在service层完成，这里直接append
		containerInfos = append(containerInfos, ci)
	}

	return containerInfos, nil
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

// ProcessContainer 处理容器
func (d *DockerRuntime) ProcessContainer(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(d.config.DataMount)
	if err != nil {
		log.Printf("Failed to get major:minor for container %s: %v", container.ID, err)
		return err
	}

	cgroupPath := d.cgroup.BuildCgroupPath(container.ID, container.CgroupParent)

	if err := d.cgroup.SetIOPSLimit(cgroupPath, majMin, d.config.ContainerIOPSLimit); err != nil {
		log.Printf("Failed to set IOPS limit for container %s: %v", container.ID, err)
		return err
	}
	log.Printf("Successfully set IOPS limit for container %s: %s %d", container.Name, majMin, d.config.ContainerIOPSLimit)

	return nil
}

// Close 关闭Docker客户端连接
func (d *DockerRuntime) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}

// SetIOPSLimit 动态设置IOPS限制
func (d *DockerRuntime) SetIOPSLimit(container *container.ContainerInfo, iopsLimit int) error {
	majMin, err := device.GetMajMin(d.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath := d.cgroup.BuildCgroupPath(container.ID, container.CgroupParent)
	return d.cgroup.SetIOPSLimit(cgroupPath, majMin, iopsLimit)
}

// ResetIOPSLimit 解除IOPS限制
func (d *DockerRuntime) ResetIOPSLimit(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(d.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath := d.cgroup.BuildCgroupPath(container.ID, container.CgroupParent)
	return d.cgroup.ResetIOPSLimit(cgroupPath, majMin)
}
