package smartlimit

import (
	"fmt"
	"log"
	"sync"
	"time"

	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/kubelet"

	corev1 "k8s.io/api/core/v1"
)

// SmartLimitConfig 智能限速配置
type SmartLimitConfig struct {
	Enabled           bool          `json:"enabled"`
	MonitorInterval   time.Duration `json:"monitor_interval"`   // 监控间隔
	HistoryWindow     time.Duration `json:"history_window"`     // 历史数据窗口
	HighIOThreshold   float64       `json:"high_io_threshold"`  // 高IO阈值（百分比）
	HighBPSThreshold  float64       `json:"high_bps_threshold"` // 高BPS阈值（字节/秒）
	AutoLimitIOPS     int           `json:"auto_limit_iops"`    // 自动限速的IOPS值
	AutoLimitBPS      int           `json:"auto_limit_bps"`     // 自动限速的BPS值
	AnnotationPrefix  string        `json:"annotation_prefix"`  // 注解前缀
	ExcludeNamespaces []string      `json:"exclude_namespaces"` // 排除的命名空间

	// kubelet API配置
	UseKubeletAPI     bool   `json:"use_kubelet_api"`     // 是否使用kubelet API
	KubeletHost       string `json:"kubelet_host"`        // kubelet主机地址
	KubeletPort       string `json:"kubelet_port"`        // kubelet端口
	KubeletTokenPath  string `json:"kubelet_token_path"`  // kubelet token路径
	KubeletCAPath     string `json:"kubelet_ca_path"`     // kubelet CA证书路径
	KubeletSkipVerify bool   `json:"kubelet_skip_verify"` // 是否跳过证书验证
}

// ContainerIOHistory 容器IO历史数据
type ContainerIOHistory struct {
	ContainerID string
	PodName     string
	Namespace   string
	Stats       []*cgroup.IOStats
	LastUpdate  time.Time
	mu          sync.RWMutex
}

// SmartLimitManager 智能限速管理器
type SmartLimitManager struct {
	config        *SmartLimitConfig
	cgroupMgr     *cgroup.Manager
	kubeClient    kubeclient.IKubeClient
	kubeletClient *kubelet.KubeletClient
	history       map[string]*ContainerIOHistory
	mu            sync.RWMutex
	stopCh        chan struct{}
}

// NewSmartLimitManager 创建智能限速管理器
func NewSmartLimitManager(config *SmartLimitConfig, cgroupMgr *cgroup.Manager, kubeClient kubeclient.IKubeClient) *SmartLimitManager {
	manager := &SmartLimitManager{
		config:     config,
		cgroupMgr:  cgroupMgr,
		kubeClient: kubeClient,
		history:    make(map[string]*ContainerIOHistory),
		stopCh:     make(chan struct{}),
	}

	// 如果启用kubelet API，则创建kubelet客户端
	if config.UseKubeletAPI && config.KubeletHost != "" && config.KubeletPort != "" {
		kubeletClient, err := kubelet.NewKubeletClient(
			config.KubeletHost,
			config.KubeletPort,
			config.KubeletTokenPath,
			config.KubeletCAPath,
			config.KubeletSkipVerify,
		)
		if err != nil {
			log.Printf("Failed to create kubelet client: %v", err)
		} else {
			manager.kubeletClient = kubeletClient
			log.Printf("Kubelet client initialized for host: %s:%s", config.KubeletHost, config.KubeletPort)
		}
	}

	return manager
}

// Start 启动智能限速监控
func (m *SmartLimitManager) Start() error {
	if !m.config.Enabled {
		log.Println("Smart limit is disabled")
		return nil
	}

	log.Printf("Starting smart limit monitoring with interval: %v", m.config.MonitorInterval)

	go m.monitorLoop()
	go m.cleanupLoop()

	return nil
}

// Stop 停止智能限速监控
func (m *SmartLimitManager) Stop() {
	close(m.stopCh)
}

// monitorLoop 监控循环
func (m *SmartLimitManager) monitorLoop() {
	ticker := time.NewTicker(m.config.MonitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.collectIOStats()
			m.analyzeAndLimit()
		case <-m.stopCh:
			return
		}
	}
}

// cleanupLoop 清理循环
func (m *SmartLimitManager) cleanupLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanupHistory()
		case <-m.stopCh:
			return
		}
	}
}

// collectIOStats 收集IO统计信息
func (m *SmartLimitManager) collectIOStats() {
	// 优先使用kubelet API
	if m.kubeletClient != nil {
		m.collectIOStatsFromKubelet()
		return
	}

	// 回退到cgroup方式
	m.collectIOStatsFromCgroup()
}

// collectIOStatsFromKubelet 从kubelet API收集IO统计信息
func (m *SmartLimitManager) collectIOStatsFromKubelet() {
	// 尝试获取节点摘要
	summary, err := m.kubeletClient.GetNodeSummary()
	if err != nil {
		log.Printf("Failed to get node summary from kubelet: %v, falling back to cgroup", err)
		m.collectIOStatsFromCgroup()
		return
	}

	// 处理每个Pod的容器统计
	for _, podStats := range summary.Pods {
		podName := podStats.PodRef.Name
		namespace := podStats.PodRef.Namespace

		// 检查是否应该监控这个Pod
		if !m.shouldMonitorPodByNamespace(namespace) {
			continue
		}

		for _, containerStats := range podStats.Containers {
			if containerStats.DiskIO == nil {
				continue
			}

			// 转换统计信息
			stats := &cgroup.IOStats{
				ContainerID: containerStats.Name,
				Timestamp:   containerStats.Timestamp,
				ReadIOPS:    int64(containerStats.DiskIO.ReadIOPS),
				WriteIOPS:   int64(containerStats.DiskIO.WriteIOPS),
				ReadBPS:     int64(containerStats.DiskIO.ReadBytes),
				WriteBPS:    int64(containerStats.DiskIO.WriteBytes),
			}

			m.addIOStats(containerStats.Name, podName, namespace, stats)
		}
	}

	// 如果节点摘要中没有足够的IO数据，尝试使用cAdvisor指标
	if len(summary.Pods) == 0 {
		m.collectIOStatsFromCadvisor()
	}
}

// collectIOStatsFromCadvisor 从cAdvisor指标收集IO统计信息
func (m *SmartLimitManager) collectIOStatsFromCadvisor() {
	metrics, err := m.kubeletClient.GetCadvisorMetrics()
	if err != nil {
		log.Printf("Failed to get cadvisor metrics: %v", err)
		return
	}

	parsedMetrics, err := m.kubeletClient.ParseCadvisorMetrics(metrics)
	if err != nil {
		log.Printf("Failed to parse cadvisor metrics: %v", err)
		return
	}

	// 获取当前节点上的Pod列表
	pods, err := m.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		log.Printf("Failed to get node pods: %v", err)
		return
	}

	for _, pod := range pods {
		if !m.shouldMonitorPod(pod) {
			continue
		}

		for _, container := range pod.Status.ContainerStatuses {
			if container.ContainerID == "" {
				continue
			}

			containerID := parseContainerID(container.ContainerID)

			// 从cAdvisor指标中查找容器数据
			stats := m.kubeletClient.ConvertCadvisorToIOStats(parsedMetrics, containerID)
			if stats != nil {
				m.addIOStats(containerID, pod.Name, pod.Namespace, stats)
			}
		}
	}
}

// collectIOStatsFromCgroup 从cgroup收集IO统计信息（原有方法）
func (m *SmartLimitManager) collectIOStatsFromCgroup() {
	pods, err := m.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		log.Printf("Failed to get node pods: %v", err)
		return
	}

	for _, pod := range pods {
		if !m.shouldMonitorPod(pod) {
			continue
		}

		for _, container := range pod.Status.ContainerStatuses {
			if container.ContainerID == "" {
				continue
			}

			containerID := parseContainerID(container.ContainerID)
			cgroupPath := m.cgroupMgr.FindCgroupPath(containerID)

			if cgroupPath == "" {
				continue
			}

			stats, err := m.cgroupMgr.GetIOStats(cgroupPath)
			if err != nil {
				log.Printf("Failed to get IO stats for container %s: %v", containerID, err)
				continue
			}

			stats.ContainerID = containerID
			m.addIOStats(containerID, pod.Name, pod.Namespace, stats)
		}
	}
}

// addIOStats 添加IO统计信息到历史记录
func (m *SmartLimitManager) addIOStats(containerID, podName, namespace string, stats *cgroup.IOStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	history, exists := m.history[containerID]
	if !exists {
		history = &ContainerIOHistory{
			ContainerID: containerID,
			PodName:     podName,
			Namespace:   namespace,
			Stats:       make([]*cgroup.IOStats, 0),
		}
		m.history[containerID] = history
	}

	history.mu.Lock()
	defer history.mu.Unlock()

	history.Stats = append(history.Stats, stats)
	history.LastUpdate = time.Now()

	// 清理过期数据
	m.cleanupContainerHistory(history)
}

// cleanupContainerHistory 清理容器的历史数据
func (m *SmartLimitManager) cleanupContainerHistory(history *ContainerIOHistory) {
	cutoff := time.Now().Add(-m.config.HistoryWindow)

	// 找到第一个未过期的数据点
	validIndex := 0
	for i, stat := range history.Stats {
		if stat.Timestamp.After(cutoff) {
			validIndex = i
			break
		}
	}

	// 保留未过期的数据
	if validIndex > 0 {
		history.Stats = history.Stats[validIndex:]
	}
}

// cleanupHistory 清理过期的历史记录
func (m *SmartLimitManager) cleanupHistory() {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-m.config.HistoryWindow)

	for containerID, history := range m.history {
		history.mu.RLock()
		lastUpdate := history.LastUpdate
		history.mu.RUnlock()

		if lastUpdate.Before(cutoff) {
			delete(m.history, containerID)
		}
	}
}

// analyzeAndLimit 分析IO趋势并执行限速
func (m *SmartLimitManager) analyzeAndLimit() {
	m.mu.RLock()
	containers := make([]string, 0, len(m.history))
	for containerID := range m.history {
		containers = append(containers, containerID)
	}
	m.mu.RUnlock()

	for _, containerID := range containers {
		m.analyzeContainer(containerID)
	}
}

// analyzeContainer 分析单个容器的IO趋势
func (m *SmartLimitManager) analyzeContainer(containerID string) {
	m.mu.RLock()
	history, exists := m.history[containerID]
	m.mu.RUnlock()

	if !exists {
		return
	}

	history.mu.RLock()
	stats := make([]*cgroup.IOStats, len(history.Stats))
	copy(stats, history.Stats)
	history.mu.RUnlock()

	if len(stats) < 2 {
		return
	}

	// 计算IO趋势
	trend := m.calculateIOTrend(stats)

	// 检查是否需要限速
	if m.shouldApplyLimit(trend) {
		m.applySmartLimit(history.PodName, history.Namespace, trend)
	}
}

// IOTrend IO趋势分析结果
type IOTrend struct {
	ReadIOPS15m  float64
	WriteIOPS15m float64
	ReadBPS15m   float64
	WriteBPS15m  float64
	ReadIOPS30m  float64
	WriteIOPS30m float64
	ReadBPS30m   float64
	WriteBPS30m  float64
	ReadIOPS60m  float64
	WriteIOPS60m float64
	ReadBPS60m   float64
	WriteBPS60m  float64
}

// calculateIOTrend 计算IO趋势
func (m *SmartLimitManager) calculateIOTrend(stats []*cgroup.IOStats) *IOTrend {
	trend := &IOTrend{}

	now := time.Now()

	// 计算15分钟、30分钟、60分钟的平均IOPS和BPS
	intervals := []struct {
		duration  time.Duration
		readIOPS  *float64
		writeIOPS *float64
		readBPS   *float64
		writeBPS  *float64
	}{
		{15 * time.Minute, &trend.ReadIOPS15m, &trend.WriteIOPS15m, &trend.ReadBPS15m, &trend.WriteBPS15m},
		{30 * time.Minute, &trend.ReadIOPS30m, &trend.WriteIOPS30m, &trend.ReadBPS30m, &trend.WriteBPS30m},
		{60 * time.Minute, &trend.ReadIOPS60m, &trend.WriteIOPS60m, &trend.ReadBPS60m, &trend.WriteBPS60m},
	}

	for _, interval := range intervals {
		cutoff := now.Add(-interval.duration)
		var totalReadIOPS, totalWriteIOPS, totalReadBPS, totalWriteBPS int64
		var count int

		for i := 1; i < len(stats); i++ {
			if stats[i].Timestamp.After(cutoff) {
				// 计算增量
				readIOPS := stats[i].ReadIOPS - stats[i-1].ReadIOPS
				writeIOPS := stats[i].WriteIOPS - stats[i-1].WriteIOPS
				readBPS := stats[i].ReadBPS - stats[i-1].ReadBPS
				writeBPS := stats[i].WriteBPS - stats[i-1].WriteBPS

				timeDiff := stats[i].Timestamp.Sub(stats[i-1].Timestamp).Seconds()
				if timeDiff > 0 {
					totalReadIOPS += int64(float64(readIOPS) / timeDiff)
					totalWriteIOPS += int64(float64(writeIOPS) / timeDiff)
					totalReadBPS += int64(float64(readBPS) / timeDiff)
					totalWriteBPS += int64(float64(writeBPS) / timeDiff)
					count++
				}
			}
		}

		if count > 0 {
			*interval.readIOPS = float64(totalReadIOPS) / float64(count)
			*interval.writeIOPS = float64(totalWriteIOPS) / float64(count)
			*interval.readBPS = float64(totalReadBPS) / float64(count)
			*interval.writeBPS = float64(totalWriteBPS) / float64(count)
		}
	}

	return trend
}

// shouldApplyLimit 判断是否需要应用限速
func (m *SmartLimitManager) shouldApplyLimit(trend *IOTrend) bool {
	// 检查15分钟、30分钟、60分钟的IO是否都超过阈值
	ioThreshold := m.config.HighIOThreshold
	bpsThreshold := m.config.HighBPSThreshold

	// 计算平均IOPS和BPS
	avgReadIOPS := (trend.ReadIOPS15m + trend.ReadIOPS30m + trend.ReadIOPS60m) / 3
	avgWriteIOPS := (trend.WriteIOPS15m + trend.WriteIOPS30m + trend.WriteIOPS60m) / 3
	avgReadBPS := (trend.ReadBPS15m + trend.ReadBPS30m + trend.ReadBPS60m) / 3
	avgWriteBPS := (trend.WriteBPS15m + trend.WriteBPS30m + trend.WriteBPS60m) / 3

	// 如果平均IOPS或BPS超过阈值，则应用限速
	return avgReadIOPS > ioThreshold || avgWriteIOPS > ioThreshold ||
		avgReadBPS > bpsThreshold || avgWriteBPS > bpsThreshold
}

// applySmartLimit 应用智能限速
func (m *SmartLimitManager) applySmartLimit(podName, namespace string, trend *IOTrend) {
	// 获取Pod
	pod, err := m.kubeClient.GetPod(namespace, podName)
	if err != nil {
		log.Printf("Failed to get pod %s/%s: %v", namespace, podName, err)
		return
	}

	// 检查是否已经有智能限速注解
	if m.hasSmartLimitAnnotation(pod.Annotations) {
		log.Printf("Pod %s/%s already has smart limit annotation", namespace, podName)
		return
	}

	// 计算限速值
	limitIOPS := m.calculateLimitIOPS(trend)
	limitBPS := m.calculateLimitBPS(trend)

	// 添加注解
	annotations := make(map[string]string)
	for k, v := range pod.Annotations {
		annotations[k] = v
	}

	annotations[m.config.AnnotationPrefix+"/smart-limit"] = "true"
	annotations[m.config.AnnotationPrefix+"/auto-iops"] = fmt.Sprintf("%d", limitIOPS)
	annotations[m.config.AnnotationPrefix+"/auto-bps"] = fmt.Sprintf("%d", limitBPS)
	annotations[m.config.AnnotationPrefix+"/limit-reason"] = "high-io-detected"

	// 更新Pod
	pod.Annotations = annotations
	_, err = m.kubeClient.UpdatePod(pod)
	if err != nil {
		log.Printf("Failed to update pod %s/%s with smart limit: %v", namespace, podName, err)
		return
	}

	log.Printf("Applied smart limit to pod %s/%s: IOPS=%d, BPS=%d", namespace, podName, limitIOPS, limitBPS)
}

// hasSmartLimitAnnotation 检查是否已有智能限速注解
func (m *SmartLimitManager) hasSmartLimitAnnotation(annotations map[string]string) bool {
	_, exists := annotations[m.config.AnnotationPrefix+"/smart-limit"]
	return exists
}

// calculateLimitIOPS 计算IOPS限速值
func (m *SmartLimitManager) calculateLimitIOPS(trend *IOTrend) int {
	// 基于当前IOPS计算限速值，设置为当前平均值的80%
	avgReadIOPS := (trend.ReadIOPS15m + trend.ReadIOPS30m + trend.ReadIOPS60m) / 3
	avgWriteIOPS := (trend.WriteIOPS15m + trend.WriteIOPS30m + trend.WriteIOPS60m) / 3

	limitIOPS := int((avgReadIOPS + avgWriteIOPS) * 0.8)

	// 确保不低于最小限速值
	if limitIOPS < m.config.AutoLimitIOPS {
		limitIOPS = m.config.AutoLimitIOPS
	}

	return limitIOPS
}

// calculateLimitBPS 计算BPS限速值
func (m *SmartLimitManager) calculateLimitBPS(trend *IOTrend) int {
	// 基于当前BPS计算限速值，设置为当前平均值的80%
	avgReadBPS := (trend.ReadBPS15m + trend.ReadBPS30m + trend.ReadBPS60m) / 3
	avgWriteBPS := (trend.WriteBPS15m + trend.WriteBPS30m + trend.WriteBPS60m) / 3

	limitBPS := int((avgReadBPS + avgWriteBPS) * 0.8)

	// 确保不低于最小限速值
	if limitBPS < m.config.AutoLimitBPS {
		limitBPS = m.config.AutoLimitBPS
	}

	return limitBPS
}

// shouldMonitorPod 判断是否应该监控Pod
func (m *SmartLimitManager) shouldMonitorPod(pod corev1.Pod) bool {
	// 检查Pod状态
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	// 检查命名空间
	for _, excludeNS := range m.config.ExcludeNamespaces {
		if pod.Namespace == excludeNS {
			return false
		}
	}

	// 检查是否已有智能限速注解
	if m.hasSmartLimitAnnotation(pod.Annotations) {
		return false
	}

	return true
}

// shouldMonitorPodByNamespace 检查是否应该监控指定命名空间的Pod
func (m *SmartLimitManager) shouldMonitorPodByNamespace(namespace string) bool {
	for _, excludeNS := range m.config.ExcludeNamespaces {
		if namespace == excludeNS {
			return false
		}
	}
	return true
}

// parseContainerID 解析容器ID
func parseContainerID(k8sID string) string {
	if k8sID == "" {
		return ""
	}

	prefixes := []string{"docker://", "containerd://"}
	for _, prefix := range prefixes {
		if len(k8sID) > len(prefix) && k8sID[:len(prefix)] == prefix {
			return k8sID[len(prefix):]
		}
	}

	return k8sID
}
