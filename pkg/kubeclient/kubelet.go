package kubeclient

import (
	"KubeDiskGuard/pkg/cadvisor"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// IOStats IO统计信息
type IOStats struct {
	ContainerID  string
	Timestamp    time.Time
	ReadIOPS     int64
	WriteIOPS    int64
	ReadBPS      int64
	WriteBPS     int64
	ReadLatency  int64 // 平均读取延迟（微秒）
	WriteLatency int64 // 平均写入延迟（微秒）
}

// ContainerStats 容器统计信息（来自kubelet API）
type ContainerStats struct {
	Name      string       `json:"name"`
	Timestamp time.Time    `json:"timestamp"`
	CPU       *CPUStats    `json:"cpu,omitempty"`
	Memory    *MemoryStats `json:"memory,omitempty"`
	DiskIO    *DiskIOStats `json:"diskio,omitempty"`
}

// CPUStats CPU统计信息
type CPUStats struct {
	UsageNanoCores       uint64 `json:"usageNanoCores"`
	UsageCoreNanoSeconds uint64 `json:"usageCoreNanoSeconds"`
}

// MemoryStats 内存统计信息
type MemoryStats struct {
	UsageBytes      uint64 `json:"usageBytes"`
	WorkingSetBytes uint64 `json:"workingSetBytes"`
}

// DiskIOStats 磁盘IO统计信息
type DiskIOStats struct {
	ReadBytes  uint64 `json:"readBytes"`
	WriteBytes uint64 `json:"writeBytes"`
	ReadIOPS   uint64 `json:"readIOPS"`
	WriteIOPS  uint64 `json:"writeIOPS"`
}

// NodeSummary 节点摘要信息
type NodeSummary struct {
	Node NodeStats  `json:"node"`
	Pods []PodStats `json:"pods"`
}

// NodeStats 节点统计信息
type NodeStats struct {
	Name      string       `json:"name"`
	Timestamp time.Time    `json:"timestamp"`
	CPU       *CPUStats    `json:"cpu,omitempty"`
	Memory    *MemoryStats `json:"memory,omitempty"`
	DiskIO    *DiskIOStats `json:"diskio,omitempty"`
}

// PodStats Pod统计信息
type PodStats struct {
	PodRef     PodReference     `json:"podRef"`
	Timestamp  time.Time        `json:"timestamp"`
	Containers []ContainerStats `json:"containers"`
}

// PodReference Pod引用信息
type PodReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
}

// newKubeletHTTPClient 创建用于与Kubelet通信的HTTP客户端
func (k *KubeClient) newKubeletHTTPClient() (*http.Client, string, error) {
	var token string
	tokenPath := k.KubeletTokenPath
	if tokenPath == "" {
		tokenPath = k.SATokenPath
	}
	if tokenPath != "" {
		if b, err := os.ReadFile(tokenPath); err == nil {
			token = strings.TrimSpace(string(b))
		}
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: k.KubeletSkipVerify,
	}

	if k.KubeletCAPath != "" && !k.KubeletSkipVerify {
		if caCert, err := os.ReadFile(k.KubeletCAPath); err == nil {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				tlsConfig.RootCAs = caCertPool
				tlsConfig.InsecureSkipVerify = false
			}
		}
	}
	if k.KubeletClientCert != "" && k.KubeletClientKey != "" {
		cert, err := tls.LoadX509KeyPair(k.KubeletClientCert, k.KubeletClientKey)
		if err == nil {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return client, token, nil
}

func (k *KubeClient) doKubeletRequest(path string) ([]byte, error) {
	client, token, err := k.newKubeletHTTPClient()
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://%s:%s%s", k.KubeletHost, k.KubeletPort, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request to %s: %v", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed request to %s, status: %d, body: %s", path, resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body from %s: %v", path, err)
	}

	return body, nil
}

// GetNodePodsFromKubelet 使用kubelet API获取本节点Pod信息
func (k *KubeClient) GetNodePodsFromKubelet() ([]corev1.Pod, error) {
	body, err := k.doKubeletRequest("/pods")
	if err != nil {
		return nil, err
	}

	var podList corev1.PodList
	if err := json.Unmarshal(body, &podList); err != nil {
		return nil, fmt.Errorf("failed to decode /pods response: %v", err)
	}

	return podList.Items, nil
}

// GetNodeSummary 获取节点摘要统计
func (k *KubeClient) GetNodeSummary() (*NodeSummary, error) {
	body, err := k.doKubeletRequest("/stats/summary")
	if err != nil {
		return nil, err
	}

	var summary NodeSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return nil, fmt.Errorf("failed to decode /stats/summary response: %v", err)
	}

	return &summary, nil
}

// GetCadvisorMetrics 获取cAdvisor原始指标
func (k *KubeClient) GetCadvisorMetrics() (string, error) {
	body, err := k.doKubeletRequest("/metrics/cadvisor")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// ParseCadvisorMetrics 解析cAdvisor指标
func (k *KubeClient) ParseCadvisorMetrics(metrics string) (*cadvisor.CadvisorMetrics, error) {
	result := &cadvisor.CadvisorMetrics{
		ContainerFSCapacityBytes:    make(map[string]float64),
		ContainerFSUsageBytes:       make(map[string]float64),
		ContainerFSIoTimeSeconds:    make(map[string]float64),
		ContainerFSIoTimeWeighted:   make(map[string]float64),
		ContainerFSReadsBytesTotal:  make(map[string]float64),
		ContainerFSWritesBytesTotal: make(map[string]float64),
		ContainerFSReadsTotal:       make(map[string]float64),
		ContainerFSWritesTotal:      make(map[string]float64),
	}
	lines := strings.Split(metrics, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		metricName := parts[0]
		value, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}

		containerID := extractContainerID(metricName)
		if containerID == "" {
			continue
		}

		switch {
		case strings.HasPrefix(metricName, "container_fs_capacity_bytes"):
			result.ContainerFSCapacityBytes[containerID] = value
		case strings.HasPrefix(metricName, "container_fs_usage_bytes"):
			result.ContainerFSUsageBytes[containerID] = value
		case strings.HasPrefix(metricName, "container_fs_io_time_seconds_total"):
			result.ContainerFSIoTimeSeconds[containerID] = value
		case strings.HasPrefix(metricName, "container_fs_io_time_weighted_seconds_total"):
			result.ContainerFSIoTimeWeighted[containerID] = value
		case strings.HasPrefix(metricName, "container_fs_reads_bytes_total"):
			result.ContainerFSReadsBytesTotal[containerID] = value
		case strings.HasPrefix(metricName, "container_fs_writes_bytes_total"):
			result.ContainerFSWritesBytesTotal[containerID] = value
		case strings.HasPrefix(metricName, "container_fs_reads_total"):
			result.ContainerFSReadsTotal[containerID] = value
		case strings.HasPrefix(metricName, "container_fs_writes_total"):
			result.ContainerFSWritesTotal[containerID] = value
		}
	}
	return result, nil
}

// extractContainerID 从cAdvisor指标中提取container ID
func extractContainerID(metricName string) string {
	start := strings.Index(metricName, "id=\"")
	if start == -1 {
		return ""
	}
	start += 4 // "id=\""
	end := strings.Index(metricName[start:], "\"")
	if end == -1 {
		return ""
	}

	// 兼容 containerd 和 docker
	// containerd: /kubepods/burstable/pod.../cri-containerd-....
	// docker: /kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod....slice/docker-...
	id := metricName[start : start+end]
	parts := strings.Split(id, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := parts[i]
		if strings.HasPrefix(part, "cri-containerd-") {
			return strings.TrimPrefix(part, "cri-containerd-")
		}
		if strings.HasPrefix(part, "docker-") {
			return strings.TrimPrefix(part, "docker-")
		}
	}

	return ""
}

// GetCadvisorIORate a new method for KubeClient to use the calculator
func (k *KubeClient) GetCadvisorIORate(containerID string, window time.Duration) (*cadvisor.IORate, error) {
	metrics, err := k.GetCadvisorMetrics()
	if err != nil {
		return nil, err
	}
	cadvisorMetrics, err := k.ParseCadvisorMetrics(metrics)
	if err != nil {
		return nil, err
	}
	k.cadvisorCalc.Update(cadvisorMetrics, time.Now())

	return k.cadvisorCalc.GetRate(containerID, window)
}

// GetCadvisorAverageIORate calculates the average IO rate over multiple windows.
func (k *KubeClient) GetCadvisorAverageIORate(containerID string, windows []time.Duration) (*cadvisor.IORate, error) {
	return k.cadvisorCalc.GetAverageRate(containerID, windows)
}

// CleanupCadvisorData cleans up old data points from the calculator.
func (k *KubeClient) CleanupCadvisorData(maxAge time.Duration) {
	k.cadvisorCalc.Cleanup(maxAge)
}

// GetCadvisorStats returns statistics about the data held in the calculator.
func (k *KubeClient) GetCadvisorStats() (containerCount, dataPointCount int) {
	return k.cadvisorCalc.Stats()
}

// ConvertCadvisorToIOStats converts cadvisor metrics to IOStats
func (k *KubeClient) ConvertCadvisorToIOStats(metrics *cadvisor.CadvisorMetrics, containerID string) *IOStats {
	rate, err := k.GetCadvisorIORate(containerID, 15*time.Second) // Using a default window
	if err != nil {
		return nil
	}
	return &IOStats{
		ContainerID: containerID,
		Timestamp:   rate.Timestamp,
		ReadIOPS:    int64(rate.ReadIOPS),
		WriteIOPS:   int64(rate.WriteIOPS),
		ReadBPS:     int64(rate.ReadBPS),
		WriteBPS:    int64(rate.WriteBPS),
	}
}
