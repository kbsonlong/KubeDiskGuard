package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"KubeDiskGuard/pkg/smartlimit"

	"github.com/gorilla/mux"
)

// APIServer HTTP API 服务器
type APIServer struct {
	smartLimitManager *smartlimit.SmartLimitManager
}

// NewAPIServer 创建新的API服务器
func NewAPIServer(smartLimitManager *smartlimit.SmartLimitManager) *APIServer {
	return &APIServer{
		smartLimitManager: smartLimitManager,
	}
}

// RegisterRoutes 注册API路由到给定的路由器
func (s *APIServer) RegisterRoutes(router *mux.Router) {
	// 创建API子路由
	apiRouter := router.PathPrefix("/api/v1").Subrouter()

	// 添加中间件
	apiRouter.Use(s.corsMiddleware)
	apiRouter.Use(s.loggingMiddleware)

	// 容器指标相关路由
	apiRouter.HandleFunc("/containers", s.handleGetAllContainerMetrics).Methods("GET")
	apiRouter.HandleFunc("/containers/{id}", s.handleGetContainerMetrics).Methods("GET")

	// 限速状态相关路由
	apiRouter.HandleFunc("/limit-status", s.handleGetAllLimitStatus).Methods("GET")
	apiRouter.HandleFunc("/limit-status/{id}", s.handleGetContainerLimitStatus).Methods("GET")

	// 系统信息路由
	apiRouter.HandleFunc("/health", s.handleHealth).Methods("GET")
	apiRouter.HandleFunc("/info", s.handleInfo).Methods("GET")
}



// corsMiddleware CORS 中间件
func (s *APIServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware 日志中间件
func (s *APIServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		duration := time.Since(start)
		log.Printf("[API] %s %s - %v", r.Method, r.URL.Path, duration)
	})
}

// writeJSONResponse 写入 JSON 响应
func (s *APIServer) writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[API] Error encoding JSON response: %v", err)
	}
}

// writeErrorResponse 写入错误响应
func (s *APIServer) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	errorResp := map[string]interface{}{
		"error":   true,
		"message": message,
		"code":    statusCode,
	}
	s.writeJSONResponse(w, errorResp, statusCode)
}

// getContainerIDFromPath 从路径中提取容器 ID
func (s *APIServer) getContainerIDFromPath(r *http.Request) string {
	vars := mux.Vars(r)
	return vars["containerID"]
}

// parseQueryParams 解析查询参数
func (s *APIServer) parseQueryParams(r *http.Request) map[string]string {
	params := make(map[string]string)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			params[key] = values[0]
		}
	}
	return params
}

// parseLimitParam 解析 limit 参数
func (s *APIServer) parseLimitParam(params map[string]string, defaultLimit int) int {
	if limitStr, exists := params["limit"]; exists {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			return limit
		}
	}
	return defaultLimit
}

// parseTimeRangeParams 解析时间范围参数
func (s *APIServer) parseTimeRangeParams(params map[string]string) (time.Time, time.Time, error) {
	now := time.Now()
	startTime := now.Add(-1 * time.Hour) // 默认 1 小时前
	endTime := now

	if startStr, exists := params["start"]; exists {
		if start, err := time.Parse(time.RFC3339, startStr); err == nil {
			startTime = start
		} else {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start time format: %s", startStr)
		}
	}

	if endStr, exists := params["end"]; exists {
		if end, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = end
		} else {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end time format: %s", endStr)
		}
	}

	return startTime, endTime, nil
}

// filterContainersByNamespace 根据命名空间过滤容器
func (s *APIServer) filterContainersByNamespace(containers map[string]*smartlimit.ContainerIOHistory, namespace string) map[string]*smartlimit.ContainerIOHistory {
	if namespace == "" {
		return containers
	}

	filtered := make(map[string]*smartlimit.ContainerIOHistory)
	for id, container := range containers {
		if container.Namespace == namespace {
			filtered[id] = container
		}
	}
	return filtered
}

// filterContainersByPod 根据 Pod 名称过滤容器
func (s *APIServer) filterContainersByPod(containers map[string]*smartlimit.ContainerIOHistory, podName string) map[string]*smartlimit.ContainerIOHistory {
	if podName == "" {
		return containers
	}

	filtered := make(map[string]*smartlimit.ContainerIOHistory)
	for id, container := range containers {
		if strings.Contains(container.PodName, podName) {
			filtered[id] = container
		}
	}
	return filtered
}