package api

import (
	"net/http"
	"strings"
	"time"

	"KubeDiskGuard/pkg/smartlimit"
)

// ContainerMetricsResponse 容器指标响应
type ContainerMetricsResponse struct {
	ContainerID string                    `json:"container_id"`
	PodName     string                    `json:"pod_name"`
	Namespace   string                    `json:"namespace"`
	LastUpdate  time.Time                 `json:"last_update"`
	Trend       *smartlimit.IOTrend       `json:"trend,omitempty"`
	History     []ContainerIOStatsHistory `json:"history,omitempty"`
}

// ContainerIOStatsHistory IO 统计历史
type ContainerIOStatsHistory struct {
	Timestamp time.Time `json:"timestamp"`
	ReadIOPS  int64     `json:"read_iops"`
	WriteIOPS int64     `json:"write_iops"`
	ReadBPS   int64     `json:"read_bps"`
	WriteBPS  int64     `json:"write_bps"`
}

// ContainerLimitStatusResponse 容器限速状态响应
type ContainerLimitStatusResponse struct {
	ContainerID string                   `json:"container_id"`
	PodName     string                   `json:"pod_name"`
	Namespace   string                   `json:"namespace"`
	IsLimited   bool                     `json:"is_limited"`
	TriggeredBy string                   `json:"triggered_by,omitempty"`
	LimitResult *smartlimit.LimitResult  `json:"limit_result,omitempty"`
	AppliedAt   *time.Time               `json:"applied_at,omitempty"`
	LastCheckAt *time.Time               `json:"last_check_at,omitempty"`
}

// APIResponse 通用 API 响应
type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Message string      `json:"message,omitempty"`
	Count   int         `json:"count,omitempty"`
}

// handleGetAllContainerMetrics 获取所有容器的指标
func (s *APIServer) handleGetAllContainerMetrics(w http.ResponseWriter, r *http.Request) {
	params := s.parseQueryParams(r)
	limit := s.parseLimitParam(params, 100)
	includeTrend := params["include_trend"] == "true"
	includeHistory := params["include_history"] == "true"
	namespace := params["namespace"]
	podName := params["pod"]

	// 获取所有容器历史数据
	allHistory := s.smartLimitManager.GetAllContainerHistory()

	// 获取所有容器趋势
	var trends map[string]*smartlimit.IOTrend
	if includeTrend {
		trends = s.smartLimitManager.AnalyzeAllContainerTrends()
	}

	var containers []ContainerMetricsResponse
	count := 0

	for containerID, history := range allHistory {
		if count >= limit {
			break
		}

		// 应用过滤器
		if namespace != "" && history.Namespace != namespace {
			continue
		}
		if podName != "" && !strings.Contains(history.PodName, podName) {
			continue
		}

		response := ContainerMetricsResponse{
			ContainerID: containerID,
			PodName:     history.PodName,
			Namespace:   history.Namespace,
			LastUpdate:  history.LastUpdate,
		}

		if includeTrend {
			if trend, exists := trends[containerID]; exists {
				response.Trend = trend
			}
		}

		if includeHistory {
			// 转换历史数据格式
			for _, stat := range history.Stats {
				response.History = append(response.History, ContainerIOStatsHistory{
					Timestamp: stat.Timestamp,
					ReadIOPS:  stat.ReadIOPS,
					WriteIOPS: stat.WriteIOPS,
					ReadBPS:   stat.ReadBPS,
					WriteBPS:  stat.WriteBPS,
				})
			}
		}

		containers = append(containers, response)
		count++
	}

	s.writeJSONResponse(w, APIResponse{
		Success: true,
		Data:    containers,
		Count:   len(containers),
	}, http.StatusOK)
}

// handleGetContainerMetrics 获取单个容器的指标
func (s *APIServer) handleGetContainerMetrics(w http.ResponseWriter, r *http.Request) {
	containerID := s.getContainerIDFromPath(r)
	if containerID == "" {
		s.writeErrorResponse(w, "Container ID is required", http.StatusBadRequest)
		return
	}

	params := s.parseQueryParams(r)
	includeTrend := params["include_trend"] == "true"
	includeHistory := params["include_history"] == "true"

	// 获取容器历史数据
	history, exists := s.smartLimitManager.GetContainerHistory(containerID)
	if !exists {
		s.writeErrorResponse(w, "Container not found", http.StatusNotFound)
		return
	}

	response := ContainerMetricsResponse{
		ContainerID: containerID,
		PodName:     history.PodName,
		Namespace:   history.Namespace,
		LastUpdate:  history.LastUpdate,
	}

	if includeTrend {
		// 获取容器趋势
		trends := s.smartLimitManager.AnalyzeAllContainerTrends()
		if trend, exists := trends[containerID]; exists {
			response.Trend = trend
		}
	}

	if includeHistory {
		// 转换历史数据格式
		for _, stat := range history.Stats {
			response.History = append(response.History, ContainerIOStatsHistory{
				Timestamp: stat.Timestamp,
				ReadIOPS:  stat.ReadIOPS,
				WriteIOPS: stat.WriteIOPS,
				ReadBPS:   stat.ReadBPS,
				WriteBPS:  stat.WriteBPS,
			})
		}
	}

	s.writeJSONResponse(w, APIResponse{
		Success: true,
		Data:    response,
	}, http.StatusOK)
}

// handleGetContainerTrend 获取容器的 IO 趋势
func (s *APIServer) handleGetContainerTrend(w http.ResponseWriter, r *http.Request) {
	containerID := s.getContainerIDFromPath(r)
	if containerID == "" {
		s.writeErrorResponse(w, "Container ID is required", http.StatusBadRequest)
		return
	}

	// 获取容器趋势
	trends := s.smartLimitManager.AnalyzeAllContainerTrends()
	trend, exists := trends[containerID]
	if !exists {
		s.writeErrorResponse(w, "Container not found", http.StatusNotFound)
		return
	}

	s.writeJSONResponse(w, APIResponse{
		Success: true,
		Data:    trend,
	}, http.StatusOK)
}

// handleGetContainerStatus 获取容器状态
func (s *APIServer) handleGetContainerStatus(w http.ResponseWriter, r *http.Request) {
	containerID := s.getContainerIDFromPath(r)
	if containerID == "" {
		s.writeErrorResponse(w, "Container ID is required", http.StatusBadRequest)
		return
	}

	// 获取容器趋势和限速状态
	trends := s.smartLimitManager.AnalyzeAllContainerTrends()
	trend, trendExists := trends[containerID]

	// 这里需要从 smartlimit manager 获取限速状态
	// 由于当前没有公开的方法，我们返回基本信息
	status := map[string]interface{}{
		"container_id": containerID,
		"has_trend":    trendExists,
		"timestamp":    time.Now(),
	}

	if trendExists {
		status["trend"] = trend
	}

	s.writeJSONResponse(w, APIResponse{
		Success: true,
		Data:    status,
	}, http.StatusOK)
}

// handleGetAllLimitStatus 获取所有容器的限速状态
func (s *APIServer) handleGetAllLimitStatus(w http.ResponseWriter, r *http.Request) {
	params := s.parseQueryParams(r)
	limit := s.parseLimitParam(params, 100)
	namespace := params["namespace"]
	podName := params["pod"]
	onlyLimited := params["only_limited"] == "true"

	// 获取所有容器的限速状态
	allLimitStatus := s.smartLimitManager.GetAllLimitStatus()

	var statuses []ContainerLimitStatusResponse
	count := 0

	for containerID, limitStatus := range allLimitStatus {
		if count >= limit {
			break
		}

		// 应用过滤器
		if namespace != "" && limitStatus.Namespace != namespace {
			continue
		}
		if podName != "" && !strings.Contains(limitStatus.PodName, podName) {
			continue
		}
		if onlyLimited && !limitStatus.IsLimited {
			continue
		}

		status := ContainerLimitStatusResponse{
			ContainerID: containerID,
			PodName:     limitStatus.PodName,
			Namespace:   limitStatus.Namespace,
			IsLimited:   limitStatus.IsLimited,
			TriggeredBy: limitStatus.TriggeredBy,
			LimitResult: limitStatus.LimitResult,
		}

		if !limitStatus.AppliedAt.IsZero() {
			status.AppliedAt = &limitStatus.AppliedAt
		}
		if !limitStatus.LastCheckAt.IsZero() {
			status.LastCheckAt = &limitStatus.LastCheckAt
		}

		statuses = append(statuses, status)
		count++
	}

	s.writeJSONResponse(w, APIResponse{
		Success: true,
		Data:    statuses,
		Count:   len(statuses),
	}, http.StatusOK)
}

// handleGetContainerLimitStatus 获取单个容器的限速状态
func (s *APIServer) handleGetContainerLimitStatus(w http.ResponseWriter, r *http.Request) {
	containerID := s.getContainerIDFromPath(r)
	if containerID == "" {
		s.writeErrorResponse(w, "Container ID is required", http.StatusBadRequest)
		return
	}

	// 获取容器限速状态
	limitStatus, exists := s.smartLimitManager.GetContainerLimitStatus(containerID)
	if !exists {
		s.writeErrorResponse(w, "Container not found", http.StatusNotFound)
		return
	}

	status := ContainerLimitStatusResponse{
		ContainerID: containerID,
		PodName:     limitStatus.PodName,
		Namespace:   limitStatus.Namespace,
		IsLimited:   limitStatus.IsLimited,
		TriggeredBy: limitStatus.TriggeredBy,
		LimitResult: limitStatus.LimitResult,
	}

	if !limitStatus.AppliedAt.IsZero() {
		status.AppliedAt = &limitStatus.AppliedAt
	}
	if !limitStatus.LastCheckAt.IsZero() {
		status.LastCheckAt = &limitStatus.LastCheckAt
	}

	s.writeJSONResponse(w, APIResponse{
		Success: true,
		Data:    status,
	}, http.StatusOK)
}

// handleHealth 健康检查
func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"service":   "KubeDiskGuard API",
		"version":   "v1",
	}

	s.writeJSONResponse(w, health, http.StatusOK)
}

// handleInfo 获取服务信息
func (s *APIServer) handleInfo(w http.ResponseWriter, r *http.Request) {
	// 获取当前监控的容器数量
	trends := s.smartLimitManager.AnalyzeAllContainerTrends()

	info := map[string]interface{}{
		"service":           "KubeDiskGuard API",
		"version":           "v1",
		"timestamp":         time.Now(),
		"monitored_containers": len(trends),
		"endpoints": map[string]string{
			"metrics":      "/api/v1/metrics/containers",
			"limits":       "/api/v1/limits/status",
			"health":       "/api/v1/health",
			"info":         "/api/v1/info",
		},
	}

	s.writeJSONResponse(w, APIResponse{
		Success: true,
		Data:    info,
	}, http.StatusOK)
}