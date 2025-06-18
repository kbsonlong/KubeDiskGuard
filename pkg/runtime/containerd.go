package runtime

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/containerd/containerd"
	containerdevents "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/namespaces"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"iops-limit-service/pkg/cgroup"
	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/device"
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

// GetContainers 获取所有容器
func (c *ContainerdRuntime) GetContainers() ([]*container.ContainerInfo, error) {
	ctx := namespaces.WithNamespace(context.Background(), c.config.ContainerdNamespace)

	containers, err := c.client.Containers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containerd containers: %v", err)
	}

	var containerInfos []*container.ContainerInfo
	for _, cont := range containers {
		containerInfo, err := c.getContainerInfo(ctx, cont)
		if err != nil {
			log.Printf("Failed to get container info for %s: %v", cont.ID(), err)
			continue
		}

		if !container.ShouldSkip(containerInfo, c.config.ExcludeKeywords) {
			containerInfos = append(containerInfos, containerInfo)
		}
	}

	return containerInfos, nil
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
		ID:   cont.ID(),
		Name: info.Labels["io.kubernetes.container.name"],
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

	return containerInfo, nil
}

// WatchContainerEvents 监听容器事件
func (c *ContainerdRuntime) WatchContainerEvents() error {
	ctx := namespaces.WithNamespace(context.Background(), c.config.ContainerdNamespace)

	// 获取事件服务
	eventService := c.client.EventService()

	// 创建事件订阅（Go SDK返回的是channel）
	eventsCh, errCh := eventService.Subscribe(ctx, "type==\"task\"")

	log.Println("Started watching containerd task events...")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case envelope := <-eventsCh:
			if envelope == nil || envelope.Event == nil {
				continue
			}
			// 监听任务启动事件
			if any, ok := envelope.Event.(*anypb.Any); ok && any.TypeUrl == "containerd.events.TaskStart" {
				startEvt := &containerdevents.TaskStart{}
				if err := proto.Unmarshal(any.Value, startEvt); err == nil {
					containerID := startEvt.ContainerID
					if containerID != "" {
						// 等待一小段时间确保容器完全启动
						time.Sleep(2 * time.Second)

						containerInfo, err := c.GetContainerByID(containerID)
						if err != nil {
							log.Printf("Failed to get container info for %s: %v", containerID, err)
							continue
						}
						if !container.ShouldSkip(containerInfo, c.config.ExcludeKeywords) {
							log.Printf("Container started: %s (%s)", containerInfo.ID, containerInfo.Name)
							c.ProcessContainer(containerInfo)
						}
					}
				}
				continue
			}
		case err := <-errCh:
			if err != nil {
				log.Printf("Error from event channel: %v", err)
				return err
			}
		}
	}
}

// ProcessContainer 处理容器
func (c *ContainerdRuntime) ProcessContainer(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(c.config.DataMount)
	if err != nil {
		log.Printf("Failed to get major:minor for container %s: %v", container.ID, err)
		return err
	}

	cgroupPath := c.cgroup.FindCgroupPath(container.ID)
	if err := c.cgroup.SetIOPSLimit(cgroupPath, majMin, c.config.ContainerIOPSLimit); err != nil {
		log.Printf("Failed to set IOPS limit for container %s: %v", container.ID, err)
		return err
	}

	log.Printf("Successfully set IOPS limit for container %s: %s %d", container.ID, majMin, c.config.ContainerIOPSLimit)
	return nil
}
