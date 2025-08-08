package runtime

import (
	"context"
	"fmt"
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
	// 获取容器规格信息
	spec, err := cont.Spec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get container spec: %v", err)
	}

	containerInfo := &container.ContainerInfo{
		ID:           cont.ID(),
		Name:         info.Labels["io.kubernetes.container.name"],
		Annotations:  map[string]string{},
		CgroupParent: spec.Linux.CgroupsPath,
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

// SetLimits 统一设置IOPS和BPS限制
func (c *ContainerdRuntime) SetLimits(container *container.ContainerInfo, riops, wiops, rbps, wbps int) error {
	majMin, err := device.GetMajMin(c.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath, err := c.getCgroupPath(container.CgroupParent)
	if err != nil {
		return fmt.Errorf("failed to get cgroup path for container %s: %v", container.ID, err)
	}
	return c.cgroup.SetLimits(cgroupPath, majMin, riops, wiops, rbps, wbps)
}

// ResetLimits 统一解除所有限速
func (c *ContainerdRuntime) ResetLimits(container *container.ContainerInfo) error {
	majMin, err := device.GetMajMin(c.config.DataMount)
	if err != nil {
		return err
	}
	cgroupPath, err := c.getCgroupPath(container.CgroupParent)
	if err != nil {
		return fmt.Errorf("failed to get cgroup path for container %s: %v", container.ID, err)
	}
	return c.cgroup.ResetLimits(cgroupPath, majMin)
}

// getCgroupPath 通过containerd API获取容器的cgroup路径
func (c *ContainerdRuntime) getCgroupPath(cgroupsPath string) (string, error) {
	// 根据cgroup版本和systemd管理模式构建完整路径
	if c.config.CgroupVersion == "v1" {
		// cgroup v1: 需要指定子系统路径
		return fmt.Sprintf("/sys/fs/cgroup/blkio%s", cgroupsPath), nil
	} else {
		// cgroup v2: 统一层次结构
		// 检查是否为systemd管理的cgroup路径格式
		if c.isSystemdCgroupPath(cgroupsPath) {
			// systemd管理模式: 需要转换路径格式
			// 例如: kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice:cri-containerd:16c0f5cee8ed9
			// 转换为: /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-besteffort.slice/kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice/cri-containerd-16c0f5cee8ed9.scope/
			return c.convertSystemdCgroupPath(cgroupsPath)
		} else {
			// 非systemd管理模式: 直接拼接路径
			// 例如: /kubepods/burstable/podc9c501eb-9423-4bd6-b96f-7f10f7f4527c/f3ee04629f75567e95fae8425cb3e9b3e1c91346b1a2ddee9139c9216c713dc7
			// 转换为: /sys/fs/cgroup/kubepods/burstable/podc9c501eb-9423-4bd6-b96f-7f10f7f4527c/f3ee04629f75567e95fae8425cb3e9b3e1c91346b1a2ddee9139c9216c713dc7
			if strings.HasPrefix(cgroupsPath, "/") {
				return fmt.Sprintf("/sys/fs/cgroup%s", cgroupsPath), nil
			} else {
				return fmt.Sprintf("/sys/fs/cgroup/%s", cgroupsPath), nil
			}
		}
	}
}

// isSystemdCgroupPath 检查是否为systemd管理的cgroup路径格式
func (c *ContainerdRuntime) isSystemdCgroupPath(cgroupsPath string) bool {
	// systemd管理的路径通常包含 .slice: 格式
	return strings.Contains(cgroupsPath, ".slice:") && strings.Contains(cgroupsPath, "cri-containerd")
}

// convertSystemdCgroupPath 转换systemd管理的cgroup路径为实际文件系统路径
func (c *ContainerdRuntime) convertSystemdCgroupPath(cgroupsPath string) (string, error) {
	// 解析systemd cgroup路径格式
	// 输入格式: kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice:cri-containerd:16c0f5cee8ed9
	// 输出格式: /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-besteffort.slice/kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice/cri-containerd-16c0f5cee8ed9.scope/

	parts := strings.Split(cgroupsPath, ":")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid systemd cgroup path format: %s", cgroupsPath)
	}

	slicePath := parts[0]   // kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice
	service := parts[1]     // cri-containerd
	containerID := parts[2] // 16c0f5cee8ed9

	// 移除末尾的.slice后缀来获取纯净的slice名称
	sliceNameWithoutSuffix := strings.TrimSuffix(slicePath, ".slice")

	// 构建slice层次结构
	sliceComponents := strings.Split(sliceNameWithoutSuffix, "-")
	var pathComponents []string

	// 添加根slice
	pathComponents = append(pathComponents, "kubelet.slice")

	// 构建中间slice路径
	currentSlice := "kubelet"
	for i := 1; i < len(sliceComponents); i++ {
		currentSlice += "-" + sliceComponents[i]
		pathComponents = append(pathComponents, currentSlice+".slice")
	}

	// 添加最终的scope
	scopeName := fmt.Sprintf("%s-%s.scope", service, containerID)
	pathComponents = append(pathComponents, scopeName)

	// 构建完整路径
	fullPath := "/sys/fs/cgroup/" + strings.Join(pathComponents, "/") + "/"
	return fullPath, nil
}
