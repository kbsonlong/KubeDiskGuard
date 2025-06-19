package container

// ContainerInfo 容器信息结构体
type ContainerInfo struct {
	ID           string
	Image        string
	Name         string
	CgroupParent string
	Annotations  map[string]string
}

// Runtime 容器运行时接口
type Runtime interface {
	// GetContainerByID 根据ID获取容器信息
	GetContainerByID(containerID string) (*ContainerInfo, error)

	// ProcessContainer 处理容器
	ProcessContainer(container *ContainerInfo) error

	// Close 关闭运行时连接
	Close() error

	// 动态设置IOPS和带宽限制（统一接口）
	SetLimits(container *ContainerInfo, riops, wiops, rbps, wbps int) error
	// 解除所有限速
	ResetLimits(container *ContainerInfo) error
}
