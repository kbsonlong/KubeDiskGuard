package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Count   int         `json:"count,omitempty"`
}

func (s *APIServer) handleGetAllLimits(w http.ResponseWriter, r *http.Request) {
	limits := s.service.GetAppliedLimits()
	namespace := r.URL.Query().Get("namespace")
	pod := r.URL.Query().Get("pod")
	filtered := limits[:0]
	for _, limit := range limits {
		if namespace != "" && limit.Namespace != namespace {
			continue
		}
		if pod != "" && !strings.Contains(limit.PodName, pod) {
			continue
		}
		filtered = append(filtered, limit)
	}
	s.writeJSONResponse(w, APIResponse{Success: true, Data: filtered, Count: len(filtered)}, http.StatusOK)
}

func (s *APIServer) handleGetLimit(w http.ResponseWriter, r *http.Request) {
	containerID := mux.Vars(r)["id"]
	for _, limit := range s.service.GetAppliedLimits() {
		if limit.ContainerID == containerID {
			s.writeJSONResponse(w, APIResponse{Success: true, Data: limit}, http.StatusOK)
			return
		}
	}
	s.writeErrorResponse(w, "container limit not found", http.StatusNotFound)
}

func (s *APIServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	status := s.service.GetServiceStatus()
	code := http.StatusOK
	health := "healthy"
	if !status.LastReconcileAt.IsZero() && status.LastError != "" {
		code = http.StatusServiceUnavailable
		health = "degraded"
	}
	s.writeJSONResponse(w, map[string]interface{}{"status": health, "timestamp": time.Now(), "reconcile": status}, code)
}

func (s *APIServer) handleInfo(w http.ResponseWriter, _ *http.Request) {
	status := s.service.GetServiceStatus()
	s.writeJSONResponse(w, APIResponse{Success: true, Data: map[string]interface{}{
		"service":            "KubeDiskGuard",
		"mode":               "static-policy-executor",
		"timestamp":          time.Now(),
		"managed_containers": status.ManagedCount,
		"endpoints": map[string]string{
			"limits": "/api/v1/limits",
			"health": "/api/v1/health",
			"info":   "/api/v1/info",
		},
	}}, http.StatusOK)
}
