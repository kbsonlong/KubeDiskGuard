package container

import (
	"regexp"

	"k8s.io/apimachinery/pkg/labels"
)

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
	// GetContainers 获取所有容器
	GetContainers() ([]*ContainerInfo, error)

	// GetContainerByID 根据ID获取容器信息
	GetContainerByID(containerID string) (*ContainerInfo, error)

	// ProcessContainer 处理容器
	ProcessContainer(container *ContainerInfo) error

	// Close 关闭运行时连接
	Close() error

	// 新增：通过Pod信息查找本地容器
	GetContainersByPod(namespace, podName string) ([]*ContainerInfo, error)

	// 新增：动态设置IOPS限制
	SetIOPSLimit(container *ContainerInfo, iopsLimit int) error
}

// ShouldSkip 检查是否应该跳过容器，支持关键字、命名空间、正则、labelSelector
func ShouldSkip(
	container *ContainerInfo,
	excludeKeywords []string,
	excludeNamespaces []string,
	excludeRegexps []string,
	excludeLabelSelector string,
) bool {
	// 1. 关键字过滤（镜像名、容器名）
	for _, keyword := range excludeKeywords {
		if contains(container.Image, keyword) || contains(container.Name, keyword) {
			return true
		}
	}
	// 2. 命名空间过滤
	if ns, ok := container.Annotations["io.kubernetes.pod.namespace"]; ok {
		for _, ens := range excludeNamespaces {
			if ns == ens {
				return true
			}
		}
	}
	// 3. 正则表达式过滤
	for _, pattern := range excludeRegexps {
		if matched, _ := regexp.MatchString(pattern, container.Image); matched {
			return true
		}
		if matched, _ := regexp.MatchString(pattern, container.Name); matched {
			return true
		}
	}
	// 4. labelSelector过滤（K8s原生语法）
	if excludeLabelSelector != "" {
		sel, err := labels.Parse(excludeLabelSelector)
		if err == nil {
			if sel.Matches(labels.Set(container.Annotations)) {
				return true
			}
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
