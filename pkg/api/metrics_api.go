package api

import (
	"KubeDiskGuard/pkg/smartlimit"
)

// MetricsAPI 为 smartlimit 包提供 API 访问接口
type MetricsAPI struct {
	manager *smartlimit.SmartLimitManager
}

// NewMetricsAPI 创建新的 MetricsAPI
func NewMetricsAPI(manager *smartlimit.SmartLimitManager) *MetricsAPI {
	return &MetricsAPI{
		manager: manager,
	}
}

// GetAllContainerTrends 获取所有容器的 IO 趋势
func (api *MetricsAPI) GetAllContainerTrends() map[string]*smartlimit.IOTrend {
	return api.manager.AnalyzeAllContainerTrends()
}

// GetContainerTrend 获取单个容器的 IO 趋势
func (api *MetricsAPI) GetContainerTrend(containerID string) (*smartlimit.IOTrend, bool) {
	trends := api.manager.AnalyzeAllContainerTrends()
	trend, exists := trends[containerID]
	return trend, exists
}

// ContainerInfo 容器基本信息
type ContainerInfo struct {
	ContainerID string `json:"container_id"`
	PodName     string `json:"pod_name"`
	Namespace   string `json:"namespace"`
}

// GetContainerInfo 获取容器基本信息（需要通过反射或添加新方法到 smartlimit）
// 这是一个占位符方法，实际实现需要修改 smartlimit 包
func (api *MetricsAPI) GetContainerInfo(containerID string) (*ContainerInfo, bool) {
	// TODO: 实现从 smartlimit manager 获取容器信息的逻辑
	// 目前返回基本信息
	return &ContainerInfo{
		ContainerID: containerID,
		PodName:     "unknown",
		Namespace:   "unknown",
	}, true
}

// GetAllContainerInfo 获取所有容器的基本信息
func (api *MetricsAPI) GetAllContainerInfo() map[string]*ContainerInfo {
	trends := api.manager.AnalyzeAllContainerTrends()
	infos := make(map[string]*ContainerInfo)
	
	for containerID := range trends {
		info, _ := api.GetContainerInfo(containerID)
		infos[containerID] = info
	}
	
	return infos
}