package runtime

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
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
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
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
		}

		if !container.ShouldSkip(ci, d.config.ExcludeKeywords) {
			containerInfos = append(containerInfos, ci)
		}
	}

	return containerInfos, nil
}

// GetContainerByID 根据ID获取容器信息
func (d *DockerRuntime) GetContainerByID(containerID string) (*container.ContainerInfo, error) {
	info, err := d.client.ContainerInspect(context.Background(), containerID)
	if err != nil {
		return nil, err
	}

	return &container.ContainerInfo{
		ID:           info.ID,
		Image:        info.Config.Image,
		Name:         strings.TrimPrefix(info.Name, "/"),
		CgroupParent: info.HostConfig.CgroupParent,
	}, nil
}

// WatchContainerEvents 监听容器事件
func (d *DockerRuntime) WatchContainerEvents() error {
	// 监听容器启动事件而不是创建事件
	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("event", "start")
	ctx := context.Background()
	msgs, errs := d.client.Events(ctx, types.EventsOptions{Filters: f})

	for {
		select {
		case event := <-msgs:
			if event.Type == "container" && event.Action == "start" {
				// 等待一小段时间确保容器完全启动
				time.Sleep(2 * time.Second)

				containerInfo, err := d.GetContainerByID(event.Actor.ID)
				if err != nil {
					log.Printf("Failed to get container info for %s: %v", event.Actor.ID, err)
					continue
				}
				if !container.ShouldSkip(containerInfo, d.config.ExcludeKeywords) {
					log.Printf("Container started: %s (%s)", containerInfo.ID, containerInfo.Name)
					d.ProcessContainer(containerInfo)
				}
			}
		case err := <-errs:
			if err == io.EOF {
				log.Println("Docker events stream ended, reconnecting...")
				time.Sleep(5 * time.Second)
				return d.WatchContainerEvents()
			}
			log.Printf("Docker events error: %v", err)
			return err
		}
	}
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

	return nil
}

// Close 关闭Docker客户端连接
func (d *DockerRuntime) Close() error {
	if d.client != nil {
		return d.client.Close()
	}
	return nil
}
