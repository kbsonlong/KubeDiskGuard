package smartlimit

import (
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// parseContainerID 解析容器ID
func parseContainerID(containerID string) string {
	if len(containerID) >= 9 && containerID[:9] == "docker://" {
		return containerID[9:]
	}
	if len(containerID) >= 13 && containerID[:13] == "containerd://" {
		return containerID[13:]
	}
	return containerID
}

// parseIntAnnotation 解析整数注解
func (m *SmartLimitManager) parseIntAnnotation(value string, defaultValue int) int {
	if value == "" {
		return defaultValue
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		return parsed
	}
	return defaultValue
}

// hasLimitAnnotations 检查Pod是否有限速注解
func (m *SmartLimitManager) hasLimitAnnotations(annotations map[string]string) bool {
	if annotations == nil {
		return false
	}

	prefix := m.config.SmartLimitAnnotationPrefix + "/"
	for key := range annotations {
		if strings.HasPrefix(key, prefix) {
			// 排除解除限速的标记
			if key == prefix+"limit-removed" {
				continue
			}
			return true
		}
	}
	return false
}

// min 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// shouldMonitorPod 判断是否应该监控Pod
func (m *SmartLimitManager) shouldMonitorPod(pod corev1.Pod) bool {
	// 检查命名空间
	if !m.shouldMonitorPodByNamespace(pod.Namespace) {
		return false
	}

	// 检查标签选择器
	if m.config.ExcludeLabelSelector != "" {
		// 这里可以添加标签选择器逻辑
		return false
	}

	return true
}

// shouldMonitorPodByNamespace 根据命名空间判断是否应该监控Pod
func (m *SmartLimitManager) shouldMonitorPodByNamespace(namespace string) bool {
	for _, excludeNS := range m.config.ExcludeNamespaces {
		if namespace == excludeNS {
			return false
		}
	}
	return true
}
