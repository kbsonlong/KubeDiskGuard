package smartlimit

import (
	"log"
	"strconv"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/kubelet"
)

// ContainerIOHistory 容器IO历史记录
type ContainerIOHistory struct {
	ContainerID string
	PodName     string
	Namespace   string
	Stats       []*kubelet.IOStats
	LastUpdate  time.Time
	mu          sync.RWMutex
}

// SmartLimitManager 智能限速管理器
type SmartLimitManager struct {
	config        *config.Config
	kubeClient    *kubeclient.KubeClient
	kubeletClient *kubelet.KubeletClient
	cgroupMgr     *cgroup.Manager
	history       map[string]*ContainerIOHistory
	mu            sync.RWMutex
	stopCh        chan struct{}
}

// NewSmartLimitManager 创建智能限速管理器
func NewSmartLimitManager(config *config.Config, kubeClient *kubeclient.KubeClient, kubeletClient *kubelet.KubeletClient, cgroupMgr *cgroup.Manager) *SmartLimitManager {
	return &SmartLimitManager{
		config:        config,
		kubeClient:    kubeClient,
		kubeletClient: kubeletClient,
		cgroupMgr:     cgroupMgr,
		history:       make(map[string]*ContainerIOHistory),
		stopCh:        make(chan struct{}),
	}
}

// Start 启动智能限速管理器
func (m *SmartLimitManager) Start() {
	if !m.config.SmartLimitEnabled {
		log.Println("Smart limit is disabled")
		return
	}

	log.Println("Starting smart limit manager...")

	// 启动监控循环
	go m.monitorLoop()

	// 启动清理循环
	go m.cleanupLoop()

	log.Println("Smart limit manager started")
}

// Stop 停止智能限速管理器
func (m *SmartLimitManager) Stop() {
	log.Println("Stopping smart limit manager...")
	close(m.stopCh)
	log.Println("Smart limit manager stopped")
}

// monitorLoop 监控循环
func (m *SmartLimitManager) monitorLoop() {
	interval := time.Duration(m.config.SmartLimitMonitorInterval) * time.Second
	ticker := time.NewTicker(interval)
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
	// 使用kubelet API获取IO数据
	if m.kubeletClient != nil {
		m.collectIOStatsFromKubelet()
		return
	}

	log.Println("Kubelet client not available, skipping IO stats collection")
}

// collectIOStatsFromKubelet 从kubelet API收集IO统计信息
func (m *SmartLimitManager) collectIOStatsFromKubelet() {
	// 尝试获取节点摘要
	summary, err := m.kubeletClient.GetNodeSummary()
	if err != nil {
		log.Printf("Failed to get node summary from kubelet: %v", err)
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
			stats := &kubelet.IOStats{
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

// addIOStats 添加IO统计信息到历史记录
func (m *SmartLimitManager) addIOStats(containerID, podName, namespace string, stats *kubelet.IOStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	history, exists := m.history[containerID]
	if !exists {
		history = &ContainerIOHistory{
			ContainerID: containerID,
			PodName:     podName,
			Namespace:   namespace,
			Stats:       make([]*kubelet.IOStats, 0),
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
	cutoff := time.Now().Add(-time.Duration(m.config.SmartLimitHistoryWindow) * time.Minute)

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

	cutoff := time.Now().Add(-time.Duration(m.config.SmartLimitHistoryWindow) * time.Minute)

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
	stats := make([]*kubelet.IOStats, len(history.Stats))
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
func (m *SmartLimitManager) calculateIOTrend(stats []*kubelet.IOStats) *IOTrend {
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
	// 检查15分钟、30分钟、60分钟的IO趋势
	intervals := []struct {
		readIOPS  float64
		writeIOPS float64
		readBPS   float64
		writeBPS  float64
	}{
		{trend.ReadIOPS15m, trend.WriteIOPS15m, trend.ReadBPS15m, trend.WriteBPS15m},
		{trend.ReadIOPS30m, trend.WriteIOPS30m, trend.ReadBPS30m, trend.WriteBPS30m},
		{trend.ReadIOPS60m, trend.WriteIOPS60m, trend.ReadBPS60m, trend.WriteBPS60m},
	}

	for _, interval := range intervals {
		// 检查IOPS阈值
		if interval.readIOPS > m.config.SmartLimitHighIOThreshold || interval.writeIOPS > m.config.SmartLimitHighIOThreshold {
			return true
		}

		// 检查BPS阈值
		if interval.readBPS > m.config.SmartLimitHighBPSThreshold || interval.writeBPS > m.config.SmartLimitHighBPSThreshold {
			return true
		}
	}

	return false
}

// applySmartLimit 应用智能限速
func (m *SmartLimitManager) applySmartLimit(podName, namespace string, trend *IOTrend) {
	// 获取Pod
	pod, err := m.kubeClient.GetPod(namespace, podName)
	if err != nil {
		log.Printf("Failed to get pod %s/%s: %v", namespace, podName, err)
		return
	}

	// 构建注解
	annotations := make(map[string]string)
	for k, v := range pod.Annotations {
		annotations[k] = v
	}

	if m.config.SmartLimitAutoIOPS > 0 {
		annotations[m.config.SmartLimitAnnotationPrefix+"/io-limit"] = strconv.Itoa(m.config.SmartLimitAutoIOPS)
	}

	if m.config.SmartLimitAutoBPS > 0 {
		annotations[m.config.SmartLimitAnnotationPrefix+"/bps-limit"] = strconv.Itoa(m.config.SmartLimitAutoBPS)
	}

	// 添加趋势信息
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-iops-15m"] = strconv.FormatFloat(trend.ReadIOPS15m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-iops-15m"] = strconv.FormatFloat(trend.WriteIOPS15m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-bps-15m"] = strconv.FormatFloat(trend.ReadBPS15m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-bps-15m"] = strconv.FormatFloat(trend.WriteBPS15m, 'f', 2, 64)

	// 更新Pod注解
	pod.Annotations = annotations
	_, err = m.kubeClient.UpdatePod(pod)
	if err != nil {
		log.Printf("Failed to update pod annotations for %s/%s: %v", namespace, podName, err)
		return
	}

	log.Printf("Applied smart limit to pod %s/%s: IOPS=%d, BPS=%d", namespace, podName, m.config.SmartLimitAutoIOPS, m.config.SmartLimitAutoBPS)
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
