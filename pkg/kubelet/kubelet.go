package kubelet

import (
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

	"KubeDiskGuard/pkg/cadvisor"
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

// KubeletClient kubelet API客户端
type KubeletClient struct {
	host       string
	port       string
	client     *http.Client
	token      string
	skipVerify bool
	calculator *cadvisor.Calculator
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

// CadvisorMetrics cAdvisor指标数据
type CadvisorMetrics struct {
	ContainerFSCapacityBytes    map[string]float64 `json:"container_fs_capacity_bytes"`
	ContainerFSUsageBytes       map[string]float64 `json:"container_fs_usage_bytes"`
	ContainerFSIoTimeSeconds    map[string]float64 `json:"container_fs_io_time_seconds"`
	ContainerFSIoTimeWeighted   map[string]float64 `json:"container_fs_io_time_weighted_seconds"`
	ContainerFSReadsBytesTotal  map[string]float64 `json:"container_fs_reads_bytes_total"`
	ContainerFSWritesBytesTotal map[string]float64 `json:"container_fs_writes_bytes_total"`
	ContainerFSReadsTotal       map[string]float64 `json:"container_fs_reads_total"`
	ContainerFSWritesTotal      map[string]float64 `json:"container_fs_writes_total"`
}

// NewKubeletClient 创建kubelet客户端
func NewKubeletClient(host, port, tokenPath, caPath string, skipVerify bool) (*KubeletClient, error) {
	client := &KubeletClient{
		host:       host,
		port:       port,
		skipVerify: skipVerify,
		calculator: cadvisor.NewCalculator(),
	}

	// 读取token
	if tokenPath != "" {
		if token, err := os.ReadFile(tokenPath); err == nil {
			client.token = strings.TrimSpace(string(token))
		}
	}

	// 配置HTTP客户端
	tlsConfig := &tls.Config{
		InsecureSkipVerify: skipVerify,
	}

	// 如果提供了CA证书，则使用它
	if caPath != "" && !skipVerify {
		if caCert, err := os.ReadFile(caPath); err == nil {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				tlsConfig.RootCAs = caCertPool
				tlsConfig.InsecureSkipVerify = false
			}
		}
	}

	client.client = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return client, nil
}

// GetNodeSummary 获取节点摘要统计
func (k *KubeletClient) GetNodeSummary() (*NodeSummary, error) {
	url := fmt.Sprintf("https://%s:%s/stats/summary", k.host, k.port)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get node summary: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get node summary, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var summary NodeSummary
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, fmt.Errorf("failed to decode node summary: %v", err)
	}

	return &summary, nil
}

// GetContainerStats 获取容器统计信息
func (k *KubeletClient) GetContainerStats(namespace, podName string) ([]ContainerStats, error) {
	url := fmt.Sprintf("https://%s:%s/stats/container/%s/%s", k.host, k.port, namespace, podName)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}

	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get container stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get container stats, status: %d, body: %s", resp.StatusCode, string(body))
	}

	var stats []ContainerStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("failed to decode container stats: %v", err)
	}

	return stats, nil
}

// GetCadvisorMetrics 获取cAdvisor指标
func (k *KubeletClient) GetCadvisorMetrics() (string, error) {
	url := fmt.Sprintf("https://%s:%s/metrics/cadvisor", k.host, k.port)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	if k.token != "" {
		req.Header.Set("Authorization", "Bearer "+k.token)
	}

	resp, err := k.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to get cadvisor metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to get cadvisor metrics, status: %d, body: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %v", err)
	}

	return string(body), nil
}

// ParseCadvisorMetrics 解析cAdvisor指标
func (k *KubeletClient) ParseCadvisorMetrics(metrics string) (*CadvisorMetrics, error) {
	lines := strings.Split(metrics, "\n")
	result := &CadvisorMetrics{
		ContainerFSCapacityBytes:    make(map[string]float64),
		ContainerFSUsageBytes:       make(map[string]float64),
		ContainerFSIoTimeSeconds:    make(map[string]float64),
		ContainerFSIoTimeWeighted:   make(map[string]float64),
		ContainerFSReadsBytesTotal:  make(map[string]float64),
		ContainerFSWritesBytesTotal: make(map[string]float64),
		ContainerFSReadsTotal:       make(map[string]float64),
		ContainerFSWritesTotal:      make(map[string]float64),
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析Prometheus格式的指标
		parts := strings.Split(line, " ")
		if len(parts) != 2 {
			continue
		}

		metricName := parts[0]
		valueStr := parts[1]

		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			continue
		}

		// 提取容器ID和指标值
		if strings.Contains(metricName, "container_fs_capacity_bytes") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSCapacityBytes[containerID] = value
			}
		} else if strings.Contains(metricName, "container_fs_usage_bytes") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSUsageBytes[containerID] = value
			}
		} else if strings.Contains(metricName, "container_fs_io_time_seconds") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSIoTimeSeconds[containerID] = value
			}
		} else if strings.Contains(metricName, "container_fs_io_time_weighted_seconds") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSIoTimeWeighted[containerID] = value
			}
		} else if strings.Contains(metricName, "container_fs_reads_bytes_total") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSReadsBytesTotal[containerID] = value
			}
		} else if strings.Contains(metricName, "container_fs_writes_bytes_total") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSWritesBytesTotal[containerID] = value
			}
		} else if strings.Contains(metricName, "container_fs_reads_total") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSReadsTotal[containerID] = value
			}
		} else if strings.Contains(metricName, "container_fs_writes_total") {
			containerID := extractContainerID(metricName)
			if containerID != "" {
				result.ContainerFSWritesTotal[containerID] = value
			}
		}
	}

	return result, nil
}

// extractContainerID 从指标名称中提取容器ID
func extractContainerID(metricName string) string {
	// 示例: container_fs_capacity_bytes{container="nginx",id="/docker/1234567890abcdef"}
	// 提取id字段的值
	if strings.Contains(metricName, "id=") {
		start := strings.Index(metricName, "id=\"")
		if start != -1 {
			start += 4
			end := strings.Index(metricName[start:], "\"")
			if end != -1 {
				id := metricName[start : start+end]
				// 提取容器ID部分
				if strings.Contains(id, "/docker/") {
					return strings.TrimPrefix(id, "/docker/")
				} else if strings.Contains(id, "/containerd/") {
					return strings.TrimPrefix(id, "/containerd/")
				}
				return id
			}
		}
	}
	return ""
}

// ConvertToIOStats 将kubelet API数据转换为IOStats
func (k *KubeletClient) ConvertToIOStats(containerStats []ContainerStats, containerID string) *IOStats {
	for _, stat := range containerStats {
		if stat.Name == containerID && stat.DiskIO != nil {
			return &IOStats{
				ContainerID: containerID,
				Timestamp:   stat.Timestamp,
				ReadIOPS:    int64(stat.DiskIO.ReadIOPS),
				WriteIOPS:   int64(stat.DiskIO.WriteIOPS),
				ReadBPS:     int64(stat.DiskIO.ReadBytes),
				WriteBPS:    int64(stat.DiskIO.WriteBytes),
			}
		}
	}
	return nil
}

// ConvertCadvisorToIOStats 将cAdvisor指标转换为IOStats（使用计算器）
func (k *KubeletClient) ConvertCadvisorToIOStats(metrics *CadvisorMetrics, containerID string) *IOStats {
	now := time.Now()

	// 获取累积值
	readBytes := metrics.ContainerFSReadsBytesTotal[containerID]
	writeBytes := metrics.ContainerFSWritesBytesTotal[containerID]
	readIOPS := metrics.ContainerFSReadsTotal[containerID]
	writeIOPS := metrics.ContainerFSWritesTotal[containerID]

	// 添加到计算器
	k.calculator.AddMetricPoint(containerID, now, readIOPS, writeIOPS, readBytes, writeBytes)

	// 计算15分钟、30分钟、60分钟的平均IO速率
	windows := []time.Duration{
		15 * time.Minute,
		30 * time.Minute,
		60 * time.Minute,
	}

	rate, err := k.calculator.CalculateAverageIORate(containerID, windows)
	if err != nil {
		// 如果计算失败，返回累积值（不推荐，但作为回退）
		return &IOStats{
			ContainerID: containerID,
			Timestamp:   now,
			ReadIOPS:    int64(readIOPS),
			WriteIOPS:   int64(writeIOPS),
			ReadBPS:     int64(readBytes),
			WriteBPS:    int64(writeBytes),
		}
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

// GetCadvisorIORate 获取cAdvisor计算的IO速率
func (k *KubeletClient) GetCadvisorIORate(containerID string, window time.Duration) (*cadvisor.IORate, error) {
	return k.calculator.CalculateIORate(containerID, window)
}

// GetCadvisorAverageIORate 获取cAdvisor计算的平均IO速率
func (k *KubeletClient) GetCadvisorAverageIORate(containerID string, windows []time.Duration) (*cadvisor.IORate, error) {
	return k.calculator.CalculateAverageIORate(containerID, windows)
}

// CleanupCadvisorData 清理cAdvisor历史数据
func (k *KubeletClient) CleanupCadvisorData(maxAge time.Duration) {
	k.calculator.CleanupOldData(maxAge)
}

// GetCadvisorStats 获取cAdvisor统计信息
func (k *KubeletClient) GetCadvisorStats() (containerCount, dataPointCount int) {
	return k.calculator.GetContainerCount(), k.calculator.GetTotalDataPoints()
}
