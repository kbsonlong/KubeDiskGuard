package container

// ContainerInfo 容器信息结构体
type ContainerInfo struct {
	ID           string
	Image        string
	Name         string
	CgroupParent string
}

// Runtime 容器运行时接口
type Runtime interface {
	// GetContainers 获取所有容器
	GetContainers() ([]*ContainerInfo, error)

	// GetContainerByID 根据ID获取容器信息
	GetContainerByID(containerID string) (*ContainerInfo, error)

	// WatchContainerEvents 监听容器事件
	WatchContainerEvents() error

	// ProcessContainer 处理容器
	ProcessContainer(container *ContainerInfo) error

	// Close 关闭运行时连接
	Close() error
}

// ShouldSkip 检查是否应该跳过容器
func ShouldSkip(container *ContainerInfo, excludeKeywords []string) bool {
	for _, keyword := range excludeKeywords {
		if contains(container.Image, keyword) || contains(container.Name, keyword) {
			return true
		}
	}
	return false
}

// contains 检查字符串是否包含子字符串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))))
}

// containsSubstring 检查字符串中间是否包含子字符串
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
