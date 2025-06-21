package smartlimit

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"

	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/kubeclient"
)

// ContainerIOHistory 容器IO历史记录
type ContainerIOHistory struct {
	ContainerID string
	PodName     string
	Namespace   string
	Stats       []*kubeclient.IOStats
	LastUpdate  time.Time
	mu          sync.RWMutex
}

// LimitStatus 限速状态
type LimitStatus struct {
	ContainerID string
	PodName     string
	Namespace   string
	IsLimited   bool
	TriggeredBy string
	LimitResult *LimitResult
	AppliedAt   time.Time
	LastCheckAt time.Time
	mu          sync.RWMutex
}

// SmartLimitManager 智能限速管理器
type SmartLimitManager struct {
	config      *config.Config
	kubeClient  kubeclient.IKubeClient
	cgroupMgr   *cgroup.Manager
	history     map[string]*ContainerIOHistory
	limitStatus map[string]*LimitStatus // 限速状态跟踪
	mu          sync.RWMutex
	stopCh      chan struct{}
}

// NewSmartLimitManager 创建智能限速管理器
func NewSmartLimitManager(config *config.Config, kubeClient kubeclient.IKubeClient, cgroupMgr *cgroup.Manager) *SmartLimitManager {
	return &SmartLimitManager{
		config:      config,
		kubeClient:  kubeClient,
		cgroupMgr:   cgroupMgr,
		history:     make(map[string]*ContainerIOHistory),
		limitStatus: make(map[string]*LimitStatus),
		stopCh:      make(chan struct{}),
	}
}

// Start 启动智能限速管理器
func (m *SmartLimitManager) Start() {
	if !m.config.SmartLimitEnabled {
		log.Println("Smart limit is disabled")
		return
	}

	log.Println("Starting smart limit manager...")

	// 恢复限速状态
	go m.restoreLimitStatus()

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
	if m.kubeClient != nil {
		m.collectIOStatsFromKubelet()
		return
	}
	log.Println("KubeClient not available, skipping IO stats collection")
}

// collectIOStatsFromKubelet 从kubelet API收集IO统计信息
func (m *SmartLimitManager) collectIOStatsFromKubelet() {
	summary, err := m.kubeClient.GetNodeSummary()
	if err != nil {
		log.Printf("Failed to get node summary from kubelet: %v, falling back to cAdvisor metrics", err)
		// Fallback to individual cAdvisor metrics if summary fails
		m.collectIOStatsFromCadvisor()
		return
	}

	if len(summary.Pods) == 0 {
		log.Println("Node summary contains no pods, trying cAdvisor metrics instead.")
		m.collectIOStatsFromCadvisor()
		return
	}

	for _, podStats := range summary.Pods {
		podName := podStats.PodRef.Name
		namespace := podStats.PodRef.Namespace
		if !m.shouldMonitorPodByNamespace(namespace) {
			continue
		}
		for _, containerStats := range podStats.Containers {
			if containerStats.DiskIO == nil {
				continue
			}
			stats := &kubeclient.IOStats{
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
}

// collectIOStatsFromCadvisor 从cAdvisor指标收集IO统计信息
func (m *SmartLimitManager) collectIOStatsFromCadvisor() {
	metrics, err := m.kubeClient.GetCadvisorMetrics()
	if err != nil {
		log.Printf("Failed to get cadvisor metrics: %v", err)
		return
	}

	parsedMetrics, err := m.kubeClient.ParseCadvisorMetrics(metrics)
	if err != nil {
		log.Printf("Failed to parse cadvisor metrics: %v", err)
		return
	}

	pods, err := m.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		log.Printf("Failed to get node pods for cAdvisor mapping: %v", err)
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
			stats := m.kubeClient.ConvertCadvisorToIOStats(parsedMetrics, containerID)
			if stats != nil {
				m.addIOStats(containerID, pod.Name, pod.Namespace, stats)
			}
		}
	}
}

// addIOStats 添加IO统计信息到历史记录
func (m *SmartLimitManager) addIOStats(containerID, podName, namespace string, stats *kubeclient.IOStats) {
	m.mu.Lock()
	defer m.mu.Unlock()

	history, exists := m.history[containerID]
	if !exists {
		history = &ContainerIOHistory{
			ContainerID: containerID,
			PodName:     podName,
			Namespace:   namespace,
			Stats:       make([]*kubeclient.IOStats, 0),
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

	// 清理过期的限速状态
	limitCutoff := time.Now().Add(-time.Duration(m.config.SmartLimitHistoryWindow) * time.Minute)
	for containerID, limitStatus := range m.limitStatus {
		limitStatus.mu.RLock()
		lastCheck := limitStatus.LastCheckAt
		limitStatus.mu.RUnlock()

		if lastCheck.Before(limitCutoff) {
			delete(m.limitStatus, containerID)
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
	stats := make([]*kubeclient.IOStats, len(history.Stats))
	copy(stats, history.Stats)
	history.mu.RUnlock()

	if len(stats) < 2 {
		return
	}

	// 计算IO趋势
	trend := m.calculateIOTrend(stats)

	// 获取当前限速状态
	limitStatus := m.getLimitStatus(containerID)

	// 检查是否需要限速或解除限速
	shouldLimit, limitResult := m.shouldApplyLimit(trend)

	if shouldLimit {
		// 需要限速
		if limitStatus != nil && limitStatus.IsLimited {
			// 如果已经限速，检查是否需要更新限速值
			if m.shouldUpdateLimit(limitStatus, limitResult) {
				log.Printf("Updating limit for container %s: %s", containerID, limitResult.Reason)
				if limitResult != nil {
					m.applySmartLimitWithResult(history.PodName, history.Namespace, trend, limitResult)
				} else {
					m.applySmartLimit(history.PodName, history.Namespace, trend)
				}
				m.updateLimitStatus(containerID, history.PodName, history.Namespace, true, limitResult)
			}
		} else {
			// 新应用限速
			if limitResult != nil {
				// 使用分级限速结果
				m.applySmartLimitWithResult(history.PodName, history.Namespace, trend, limitResult)
				// 更新限速状态
				m.updateLimitStatus(containerID, history.PodName, history.Namespace, true, limitResult)
			} else {
				// 使用原有逻辑
				m.applySmartLimit(history.PodName, history.Namespace, trend)
				// 更新限速状态（使用默认值）
				m.updateLimitStatus(containerID, history.PodName, history.Namespace, true, &LimitResult{
					TriggeredBy: "legacy",
					ReadIOPS:    m.config.SmartLimitAutoIOPS,
					WriteIOPS:   m.config.SmartLimitAutoIOPS,
					ReadBPS:     m.config.SmartLimitAutoBPS,
					WriteBPS:    m.config.SmartLimitAutoBPS,
					Reason:      "Legacy threshold triggered",
				})
			}
		}
	} else if limitStatus != nil && limitStatus.IsLimited {
		// 检查是否需要解除限速
		if m.shouldRemoveLimit(trend, limitStatus) {
			m.removeSmartLimit(history.PodName, history.Namespace, trend, limitStatus)
			// 更新限速状态
			m.updateLimitStatus(containerID, history.PodName, history.Namespace, false, nil)
		} else {
			// 更新检查时间
			limitStatus.mu.Lock()
			limitStatus.LastCheckAt = time.Now()
			limitStatus.mu.Unlock()
		}
	}
}

// shouldUpdateLimit 检查是否需要更新限速值
func (m *SmartLimitManager) shouldUpdateLimit(currentStatus *LimitStatus, newResult *LimitResult) bool {
	if newResult == nil || currentStatus.LimitResult == nil {
		return false
	}

	// 检查是否触发了不同的时间窗口
	if currentStatus.TriggeredBy != newResult.TriggeredBy {
		return true
	}

	// 检查限速值是否发生变化
	current := currentStatus.LimitResult
	if current.ReadIOPS != newResult.ReadIOPS ||
		current.WriteIOPS != newResult.WriteIOPS ||
		current.ReadBPS != newResult.ReadBPS ||
		current.WriteBPS != newResult.WriteBPS {
		return true
	}

	return false
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
func (m *SmartLimitManager) calculateIOTrend(stats []*kubeclient.IOStats) *IOTrend {
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
func (m *SmartLimitManager) shouldApplyLimit(trend *IOTrend) (bool, *LimitResult) {
	if !m.config.SmartLimitGradedThresholds {
		// 使用原有逻辑
		return m.shouldApplyLimitLegacy(trend), nil
	}

	// 分级阈值逻辑
	return m.shouldApplyLimitGraded(trend)
}

// LimitResult 限速结果
type LimitResult struct {
	TriggeredBy string // 触发限速的时间窗口
	ReadIOPS    int    // 建议的读IOPS限速值
	WriteIOPS   int    // 建议的写IOPS限速值
	ReadBPS     int    // 建议的读BPS限速值
	WriteBPS    int    // 建议的写BPS限速值
	Reason      string // 触发原因
}

// shouldApplyLimitGraded 分级阈值判断
func (m *SmartLimitManager) shouldApplyLimitGraded(trend *IOTrend) (bool, *LimitResult) {
	// 按优先级检查：15分钟 > 30分钟 > 60分钟
	// 优先使用更短时间窗口的阈值，因为短期高IO更需要立即处理

	// 检查15分钟窗口
	if m.checkWindowThreshold(trend.ReadIOPS15m, trend.WriteIOPS15m, trend.ReadBPS15m, trend.WriteBPS15m,
		m.config.SmartLimitIOThreshold15m, m.config.SmartLimitBPSThreshold15m) {

		return true, &LimitResult{
			TriggeredBy: "15m",
			ReadIOPS:    m.config.SmartLimitIOPSLimit15m,
			WriteIOPS:   m.config.SmartLimitIOPSLimit15m,
			ReadBPS:     m.config.SmartLimitBPSLimit15m,
			WriteBPS:    m.config.SmartLimitBPSLimit15m,
			Reason:      m.buildTriggerReason("15m", trend.ReadIOPS15m, trend.WriteIOPS15m, trend.ReadBPS15m, trend.WriteBPS15m),
		}
	}

	// 检查30分钟窗口
	if m.checkWindowThreshold(trend.ReadIOPS30m, trend.WriteIOPS30m, trend.ReadBPS30m, trend.WriteBPS30m,
		m.config.SmartLimitIOThreshold30m, m.config.SmartLimitBPSThreshold30m) {

		return true, &LimitResult{
			TriggeredBy: "30m",
			ReadIOPS:    m.config.SmartLimitIOPSLimit30m,
			WriteIOPS:   m.config.SmartLimitIOPSLimit30m,
			ReadBPS:     m.config.SmartLimitBPSLimit30m,
			WriteBPS:    m.config.SmartLimitBPSLimit30m,
			Reason:      m.buildTriggerReason("30m", trend.ReadIOPS30m, trend.WriteIOPS30m, trend.ReadBPS30m, trend.WriteBPS30m),
		}
	}

	// 检查60分钟窗口
	if m.checkWindowThreshold(trend.ReadIOPS60m, trend.WriteIOPS60m, trend.ReadBPS60m, trend.WriteBPS60m,
		m.config.SmartLimitIOThreshold60m, m.config.SmartLimitBPSThreshold60m) {

		return true, &LimitResult{
			TriggeredBy: "60m",
			ReadIOPS:    m.config.SmartLimitIOPSLimit60m,
			WriteIOPS:   m.config.SmartLimitIOPSLimit60m,
			ReadBPS:     m.config.SmartLimitBPSLimit60m,
			WriteBPS:    m.config.SmartLimitBPSLimit60m,
			Reason:      m.buildTriggerReason("60m", trend.ReadIOPS60m, trend.WriteIOPS60m, trend.ReadBPS60m, trend.WriteBPS60m),
		}
	}

	return false, nil
}

// checkWindowThreshold 检查单个时间窗口的阈值
func (m *SmartLimitManager) checkWindowThreshold(readIOPS, writeIOPS, readBPS, writeBPS, ioThreshold, bpsThreshold float64) bool {
	// 检查IOPS阈值
	if readIOPS > ioThreshold || writeIOPS > ioThreshold {
		return true
	}

	// 检查BPS阈值
	if readBPS > bpsThreshold || writeBPS > bpsThreshold {
		return true
	}

	return false
}

// buildTriggerReason 构建触发原因描述
func (m *SmartLimitManager) buildTriggerReason(window string, readIOPS, writeIOPS, readBPS, writeBPS float64) string {
	var reasons []string

	if readIOPS > 0 {
		reasons = append(reasons, fmt.Sprintf("ReadIOPS:%.2f", readIOPS))
	}
	if writeIOPS > 0 {
		reasons = append(reasons, fmt.Sprintf("WriteIOPS:%.2f", writeIOPS))
	}
	if readBPS > 0 {
		reasons = append(reasons, fmt.Sprintf("ReadBPS:%.2f", readBPS))
	}
	if writeBPS > 0 {
		reasons = append(reasons, fmt.Sprintf("WriteBPS:%.2f", writeBPS))
	}

	return fmt.Sprintf("%s窗口触发[%s]", window, strings.Join(reasons, ","))
}

// shouldApplyLimitLegacy 原有逻辑（兼容模式）
func (m *SmartLimitManager) shouldApplyLimitLegacy(trend *IOTrend) bool {
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

// applySmartLimitWithResult 应用分级智能限速
func (m *SmartLimitManager) applySmartLimitWithResult(podName, namespace string, trend *IOTrend, limitResult *LimitResult) {
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

	// 应用分级限速值
	if limitResult.ReadIOPS > 0 {
		annotations[m.config.SmartLimitAnnotationPrefix+"/read-iops-limit"] = strconv.Itoa(limitResult.ReadIOPS)
	}
	if limitResult.WriteIOPS > 0 {
		annotations[m.config.SmartLimitAnnotationPrefix+"/write-iops-limit"] = strconv.Itoa(limitResult.WriteIOPS)
	}
	if limitResult.ReadBPS > 0 {
		annotations[m.config.SmartLimitAnnotationPrefix+"/read-bps-limit"] = strconv.Itoa(limitResult.ReadBPS)
	}
	if limitResult.WriteBPS > 0 {
		annotations[m.config.SmartLimitAnnotationPrefix+"/write-bps-limit"] = strconv.Itoa(limitResult.WriteBPS)
	}

	// 添加触发信息
	annotations[m.config.SmartLimitAnnotationPrefix+"/triggered-by"] = limitResult.TriggeredBy
	annotations[m.config.SmartLimitAnnotationPrefix+"/trigger-reason"] = limitResult.Reason

	// 添加趋势信息
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-iops-15m"] = strconv.FormatFloat(trend.ReadIOPS15m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-iops-15m"] = strconv.FormatFloat(trend.WriteIOPS15m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-bps-15m"] = strconv.FormatFloat(trend.ReadBPS15m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-bps-15m"] = strconv.FormatFloat(trend.WriteBPS15m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-iops-30m"] = strconv.FormatFloat(trend.ReadIOPS30m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-iops-30m"] = strconv.FormatFloat(trend.WriteIOPS30m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-bps-30m"] = strconv.FormatFloat(trend.ReadBPS30m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-bps-30m"] = strconv.FormatFloat(trend.WriteBPS30m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-iops-60m"] = strconv.FormatFloat(trend.ReadIOPS60m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-iops-60m"] = strconv.FormatFloat(trend.WriteIOPS60m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-read-bps-60m"] = strconv.FormatFloat(trend.ReadBPS60m, 'f', 2, 64)
	annotations[m.config.SmartLimitAnnotationPrefix+"/trend-write-bps-60m"] = strconv.FormatFloat(trend.WriteBPS60m, 'f', 2, 64)

	// 更新Pod注解
	pod.Annotations = annotations
	_, err = m.kubeClient.UpdatePod(pod)
	if err != nil {
		log.Printf("Failed to update pod annotations for %s/%s: %v", namespace, podName, err)
		return
	}

	log.Printf("Applied graded smart limit to pod %s/%s: %s, IOPS[%d,%d], BPS[%d,%d]",
		namespace, podName, limitResult.Reason,
		limitResult.ReadIOPS, limitResult.WriteIOPS,
		limitResult.ReadBPS, limitResult.WriteBPS)
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

// getLimitStatus 获取容器限速状态
func (m *SmartLimitManager) getLimitStatus(containerID string) *LimitStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	limitStatus, exists := m.limitStatus[containerID]
	if !exists {
		return nil
	}

	return limitStatus
}

// updateLimitStatus 更新容器限速状态
func (m *SmartLimitManager) updateLimitStatus(containerID, podName, namespace string, isLimited bool, limitResult *LimitResult) {
	m.mu.Lock()
	defer m.mu.Unlock()

	limitStatus, exists := m.limitStatus[containerID]
	if !exists {
		limitStatus = &LimitStatus{
			ContainerID: containerID,
			PodName:     podName,
			Namespace:   namespace,
			IsLimited:   isLimited,
			mu:          sync.RWMutex{},
		}
		m.limitStatus[containerID] = limitStatus
	}

	limitStatus.mu.Lock()
	defer limitStatus.mu.Unlock()

	limitStatus.IsLimited = isLimited
	limitStatus.LimitResult = limitResult
	if limitResult != nil {
		limitStatus.TriggeredBy = limitResult.TriggeredBy
	}
	limitStatus.AppliedAt = time.Now()
	limitStatus.LastCheckAt = time.Now()
}

// shouldRemoveLimit 判断是否需要解除限速
func (m *SmartLimitManager) shouldRemoveLimit(trend *IOTrend, limitStatus *LimitStatus) bool {
	limitStatus.mu.RLock()
	defer limitStatus.mu.RUnlock()

	// 检查是否达到解除延迟时间
	removeDelay := time.Duration(m.config.SmartLimitRemoveDelay) * time.Minute
	if time.Since(limitStatus.AppliedAt) < removeDelay {
		return false
	}

	// 检查是否达到检查间隔
	checkInterval := time.Duration(m.config.SmartLimitRemoveCheckInterval) * time.Minute
	if time.Since(limitStatus.LastCheckAt) < checkInterval {
		return false
	}

	// 根据触发的时间窗口检查IO是否已经降低到安全水平
	switch limitStatus.TriggeredBy {
	case "15m":
		return m.checkRemoveCondition(trend.ReadIOPS15m, trend.WriteIOPS15m, trend.ReadBPS15m, trend.WriteBPS15m)
	case "30m":
		return m.checkRemoveCondition(trend.ReadIOPS30m, trend.WriteIOPS30m, trend.ReadBPS30m, trend.WriteBPS30m)
	case "60m":
		return m.checkRemoveCondition(trend.ReadIOPS60m, trend.WriteIOPS60m, trend.ReadBPS60m, trend.WriteBPS60m)
	case "legacy":
		// 对于legacy模式，检查所有时间窗口
		return m.checkRemoveCondition(
			math.Max(trend.ReadIOPS15m, math.Max(trend.ReadIOPS30m, trend.ReadIOPS60m)),
			math.Max(trend.WriteIOPS15m, math.Max(trend.WriteIOPS30m, trend.WriteIOPS60m)),
			math.Max(trend.ReadBPS15m, math.Max(trend.ReadBPS30m, trend.ReadBPS60m)),
			math.Max(trend.WriteBPS15m, math.Max(trend.WriteBPS30m, trend.WriteBPS60m)),
		)
	default:
		return false
	}
}

// checkRemoveCondition 检查解除条件
func (m *SmartLimitManager) checkRemoveCondition(readIOPS, writeIOPS, readBPS, writeBPS float64) bool {
	// 检查IOPS是否都低于解除阈值
	if readIOPS > m.config.SmartLimitRemoveThreshold || writeIOPS > m.config.SmartLimitRemoveThreshold {
		return false
	}

	// 检查BPS是否都低于解除阈值
	if readBPS > m.config.SmartLimitRemoveThreshold || writeBPS > m.config.SmartLimitRemoveThreshold {
		return false
	}

	return true
}

// removeSmartLimit 移除限速
func (m *SmartLimitManager) removeSmartLimit(podName, namespace string, trend *IOTrend, limitStatus *LimitStatus) {
	// 获取Pod
	pod, err := m.kubeClient.GetPod(namespace, podName)
	if err != nil {
		log.Printf("Failed to get pod %s/%s for removing limit: %v", namespace, podName, err)
		return
	}

	// 构建注解
	annotations := make(map[string]string)
	for k, v := range pod.Annotations {
		// 移除限速相关的注解
		if !strings.HasPrefix(k, m.config.SmartLimitAnnotationPrefix+"/") {
			annotations[k] = v
		}
	}

	// 添加解除限速的标记
	annotations[m.config.SmartLimitAnnotationPrefix+"/limit-removed"] = "true"
	annotations[m.config.SmartLimitAnnotationPrefix+"/removed-at"] = time.Now().Format(time.RFC3339)
	annotations[m.config.SmartLimitAnnotationPrefix+"/removed-reason"] = m.buildRemoveReason(trend, limitStatus)

	// 更新Pod注解
	pod.Annotations = annotations
	_, err = m.kubeClient.UpdatePod(pod)
	if err != nil {
		log.Printf("Failed to remove smart limit for pod %s/%s: %v", namespace, podName, err)
		return
	}

	log.Printf("Removed smart limit from pod %s/%s: %s", namespace, podName, m.buildRemoveReason(trend, limitStatus))
}

// buildRemoveReason 构建解除限速原因
func (m *SmartLimitManager) buildRemoveReason(trend *IOTrend, limitStatus *LimitStatus) string {
	limitStatus.mu.RLock()
	defer limitStatus.mu.RUnlock()

	var currentValues []string

	switch limitStatus.TriggeredBy {
	case "15m":
		currentValues = append(currentValues, fmt.Sprintf("ReadIOPS:%.2f", trend.ReadIOPS15m))
		currentValues = append(currentValues, fmt.Sprintf("WriteIOPS:%.2f", trend.WriteIOPS15m))
		currentValues = append(currentValues, fmt.Sprintf("ReadBPS:%.2f", trend.ReadBPS15m))
		currentValues = append(currentValues, fmt.Sprintf("WriteBPS:%.2f", trend.WriteBPS15m))
	case "30m":
		currentValues = append(currentValues, fmt.Sprintf("ReadIOPS:%.2f", trend.ReadIOPS30m))
		currentValues = append(currentValues, fmt.Sprintf("WriteIOPS:%.2f", trend.WriteIOPS30m))
		currentValues = append(currentValues, fmt.Sprintf("ReadBPS:%.2f", trend.ReadBPS30m))
		currentValues = append(currentValues, fmt.Sprintf("WriteBPS:%.2f", trend.WriteBPS30m))
	case "60m":
		currentValues = append(currentValues, fmt.Sprintf("ReadIOPS:%.2f", trend.ReadIOPS60m))
		currentValues = append(currentValues, fmt.Sprintf("WriteIOPS:%.2f", trend.WriteIOPS60m))
		currentValues = append(currentValues, fmt.Sprintf("ReadBPS:%.2f", trend.ReadBPS60m))
		currentValues = append(currentValues, fmt.Sprintf("WriteBPS:%.2f", trend.WriteBPS60m))
	default:
		currentValues = append(currentValues, "Legacy mode")
	}

	return fmt.Sprintf("IO已恢复正常[%s], 阈值:%.2f", strings.Join(currentValues, ","), m.config.SmartLimitRemoveThreshold)
}

// restoreLimitStatus 恢复限速状态
func (m *SmartLimitManager) restoreLimitStatus() {
	log.Println("Restoring limit status from pod annotations...")

	// 获取当前节点上的所有Pod
	pods, err := m.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		log.Printf("Failed to get node pods for status restoration: %v", err)
		return
	}

	restoredCount := 0
	for _, pod := range pods {
		if !m.shouldMonitorPod(pod) {
			continue
		}

		// 检查Pod是否有限速注解
		if m.hasLimitAnnotations(pod.Annotations) {
			// 为每个容器恢复限速状态
			for _, container := range pod.Status.ContainerStatuses {
				if container.ContainerID == "" {
					continue
				}

				containerID := parseContainerID(container.ContainerID)
				if m.restoreContainerLimitStatus(containerID, pod.Name, pod.Namespace, pod.Annotations) {
					restoredCount++
				}
			}
		}
	}

	log.Printf("Restored limit status for %d containers", restoredCount)
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

// restoreContainerLimitStatus 恢复单个容器的限速状态
func (m *SmartLimitManager) restoreContainerLimitStatus(containerID, podName, namespace string, annotations map[string]string) bool {
	prefix := m.config.SmartLimitAnnotationPrefix + "/"

	// 检查是否已被解除限速
	if removed, exists := annotations[prefix+"limit-removed"]; exists && removed == "true" {
		return false
	}

	// 解析触发窗口
	triggeredBy, exists := annotations[prefix+"triggered-by"]
	if !exists {
		return false
	}

	// 解析限速值
	readIOPS := m.parseIntAnnotation(annotations[prefix+"read-iops-limit"], 0)
	writeIOPS := m.parseIntAnnotation(annotations[prefix+"write-iops-limit"], 0)
	readBPS := m.parseIntAnnotation(annotations[prefix+"read-bps-limit"], 0)
	writeBPS := m.parseIntAnnotation(annotations[prefix+"write-bps-limit"], 0)

	// 解析触发原因
	reason, _ := annotations[prefix+"trigger-reason"]

	// 创建限速结果
	limitResult := &LimitResult{
		TriggeredBy: triggeredBy,
		ReadIOPS:    readIOPS,
		WriteIOPS:   writeIOPS,
		ReadBPS:     readBPS,
		WriteBPS:    writeBPS,
		Reason:      reason,
	}

	// 更新限速状态
	m.updateLimitStatus(containerID, podName, namespace, true, limitResult)

	log.Printf("Restored limit status for container %s: %s", containerID, reason)
	return true
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
