package smartlimit

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"KubeDiskGuard/pkg/annotationkeys"
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

// LimitResult 限速结果
type LimitResult struct {
	TriggeredBy string // 触发限速的时间窗口
	ReadIOPS    int    // 建议的读IOPS限速值
	WriteIOPS   int    // 建议的写IOPS限速值
	ReadBPS     int    // 建议的读BPS限速值
	WriteBPS    int    // 建议的写BPS限速值
	Reason      string // 触发原因
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

// ContainerLimit 容器限额结构体
type ContainerLimit struct {
	IOPS int
	BPS  int
}

// SmartLimitManager 智能限速管理器
type SmartLimitManager struct {
	config          *config.Config
	kubeClient      kubeclient.IKubeClient
	cgroupMgr       *cgroup.Manager
	history         map[string]*ContainerIOHistory
	limitStatus     map[string]*LimitStatus    // 限速状态跟踪
	containerLimits map[string]*ContainerLimit // containerID -> 限额
	mu              sync.RWMutex
	stopCh          chan struct{}
}

// NewSmartLimitManager 创建智能限速管理器
func NewSmartLimitManager(config *config.Config, kubeClient kubeclient.IKubeClient, cgroupMgr *cgroup.Manager) *SmartLimitManager {
	return &SmartLimitManager{
		config:          config,
		kubeClient:      kubeClient,
		cgroupMgr:       cgroupMgr,
		history:         make(map[string]*ContainerIOHistory),
		limitStatus:     make(map[string]*LimitStatus),
		stopCh:          make(chan struct{}),
		containerLimits: make(map[string]*ContainerLimit),
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

// analyzeAndLimit 拆分为分析和限速调度
func (m *SmartLimitManager) analyzeAndLimit() {
	trends := m.AnalyzeAllContainerTrends()
	m.ApplyLimitIfNeeded(trends)
}

// ApplyLimitIfNeeded 根据分析结果判断并执行限速
func (m *SmartLimitManager) ApplyLimitIfNeeded(trends map[string]*IOTrend) {
	for containerID, trend := range trends {
		m.applyLimitForContainer(containerID, trend)
	}
}

// applyLimitForContainer 根据趋势判断并执行限速
func (m *SmartLimitManager) applyLimitForContainer(containerID string, trend *IOTrend) {
	m.mu.RLock()
	history, exists := m.history[containerID]
	m.mu.RUnlock()
	if !exists {
		return
	}
	limitStatus := m.getLimitStatus(containerID)
	shouldLimit, limitResult := m.shouldApplyLimitGraded(trend)

	// 1. 需要解除限速
	if !shouldLimit && limitStatus != nil && limitStatus.IsLimited {
		if m.shouldRemoveLimit(trend, limitStatus) {
			m.removeSmartLimit(history.PodName, history.Namespace, trend, limitStatus)
			m.updateLimitStatus(containerID, history.PodName, history.Namespace, false, nil)
			removeReason := m.buildRemoveReason(trend, limitStatus)
			_ = m.kubeClient.CreateEvent(history.Namespace, history.PodName, "Normal", "SmartLimitRemoved", "解除限速原因: "+removeReason)
		} else {
			limitStatus.mu.Lock()
			limitStatus.LastCheckAt = time.Now()
			limitStatus.mu.Unlock()
		}
		return
	}

	// 2. 不需要限速，且当前未限速，直接返回
	if !shouldLimit {
		return
	}

	// 3. 需要限速，且已限速，判断是否需要更新
	if limitStatus != nil && limitStatus.IsLimited {
		if !m.shouldUpdateLimit(limitStatus, limitResult) {
			return
		}
		if limitResult != nil {
			log.Printf("Updating limit for container %s: %s", containerID, limitResult.Reason)
			m.applySmartLimitWithResult(history.PodName, history.Namespace, trend, limitResult)
			_ = m.kubeClient.CreateEvent(history.Namespace, history.PodName, "Normal", "SmartLimitUpdated", "限速更新: "+limitResult.Reason)
		} else {
			m.applySmartLimit(history.PodName, history.Namespace, trend)
		}
		m.updateLimitStatus(containerID, history.PodName, history.Namespace, true, limitResult)
		return
	}

	// 4. 需要限速，且未限速，首次限速
	if limitResult != nil {
		m.applySmartLimitWithResult(history.PodName, history.Namespace, trend, limitResult)
		m.updateLimitStatus(containerID, history.PodName, history.Namespace, true, limitResult)
		_ = m.kubeClient.CreateEvent(history.Namespace, history.PodName, "Normal", "SmartLimitApplied", "限速原因: "+limitResult.Reason)
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

// 判断是否需要应用限速
// shouldApplyLimitGraded 分级阈值判断
func (m *SmartLimitManager) shouldApplyLimitGraded(trend *IOTrend) (bool, *LimitResult) {
	// 按优先级检查：15分钟 > 30分钟 > 60分钟
	// 优先使用更短时间窗口的阈值，因为短期高IO更需要立即处理
	// Todo: 调整算法
	// 1. avg_with_window < setmax , use avg_with_window
	// 2. avg_with_window > setmax , use setmax

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

// 获取容器限额（无则分配默认值）
func (m *SmartLimitManager) getOrInitContainerLimit(containerID string) *ContainerLimit {
	m.mu.Lock()
	defer m.mu.Unlock()
	limit, exists := m.containerLimits[containerID]
	if !exists {
		limit = &ContainerLimit{
			IOPS: m.config.DefaultIOPSLimit,
			BPS:  m.config.DefaultBPSLimit,
		}
		m.containerLimits[containerID] = limit
	}
	// 限额不超过最大值
	if limit.IOPS > m.config.MaxIOPSLimit {
		limit.IOPS = m.config.MaxIOPSLimit
	}
	if limit.BPS > m.config.MaxBPSLimit {
		limit.BPS = m.config.MaxBPSLimit
	}
	return limit
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
		annotations[m.config.SmartLimitAnnotationPrefix+"/iops-limit"] = strconv.Itoa(m.config.SmartLimitAutoIOPS)
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
	// 获取容器限额
	containerID := podName + ":" + namespace // 简化，实际应用时可用真实containerID
	limit := m.getOrInitContainerLimit(containerID)

	// 融合分级限速与全局限额逻辑
	var readIOPS, writeIOPS, readBPS, writeBPS int
	if limitResult != nil && (limitResult.ReadIOPS > 0 || limitResult.ReadBPS > 0) {
		// 分级限速优先，且不超过全局最大
		readIOPS = min(limitResult.ReadIOPS, m.config.MaxIOPSLimit)
		writeIOPS = min(limitResult.WriteIOPS, m.config.MaxIOPSLimit)
		readBPS = min(limitResult.ReadBPS, m.config.MaxBPSLimit)
		writeBPS = min(limitResult.WriteBPS, m.config.MaxBPSLimit)
	} else {
		// 否则用containerLimits
		readIOPS = min(limit.IOPS, m.config.MaxIOPSLimit)
		writeIOPS = min(limit.IOPS, m.config.MaxIOPSLimit)
		readBPS = min(limit.BPS, m.config.MaxBPSLimit)
		writeBPS = min(limit.BPS, m.config.MaxBPSLimit)
	}

	prefix := m.config.SmartLimitAnnotationPrefix
	// 检查当前注解中是否有0值，若有则本轮跳过下发该项
	if !(annotations[prefix+"/"+annotationkeys.ReadIopsAnnotationKey] == "0") && readIOPS > 0 {
		annotations[prefix+"/"+annotationkeys.ReadIopsAnnotationKey] = strconv.Itoa(readIOPS)
	}
	if !(annotations[prefix+"/"+annotationkeys.WriteIopsAnnotationKey] == "0") && writeIOPS > 0 {
		annotations[prefix+"/"+annotationkeys.WriteIopsAnnotationKey] = strconv.Itoa(writeIOPS)
	}
	if !(annotations[prefix+"/"+annotationkeys.ReadBpsAnnotationKey] == "0") && readBPS > 0 {
		annotations[prefix+"/"+annotationkeys.ReadBpsAnnotationKey] = strconv.Itoa(readBPS)
	}
	if !(annotations[prefix+"/"+annotationkeys.WriteBpsAnnotationKey] == "0") && writeBPS > 0 {
		annotations[prefix+"/"+annotationkeys.WriteBpsAnnotationKey] = strconv.Itoa(writeBPS)
	}
	// 添加触发信息
	if limitResult != nil {
		annotations[prefix+"/triggered-by"] = limitResult.TriggeredBy
		annotations[prefix+"/trigger-reason"] = limitResult.Reason
	}
	// 添加趋势信息（略）
	pod.Annotations = annotations
	_, err = m.kubeClient.UpdatePod(pod)
	if err != nil {
		log.Printf("Failed to update pod annotations for %s/%s: %v", namespace, podName, err)
		return
	}
	log.Printf("Applied smart limit to pod %s/%s: IOPS[%d,%d], BPS[%d,%d]", namespace, podName, readIOPS, writeIOPS, readBPS, writeIOPS)
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

// GetAllLimitStatus 获取所有容器的限速状态（API 接口）
func (m *SmartLimitManager) GetAllLimitStatus() map[string]*LimitStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*LimitStatus)
	for containerID, status := range m.limitStatus {
		// 创建副本以避免并发问题
		statusCopy := &LimitStatus{
			ContainerID: status.ContainerID,
			PodName:     status.PodName,
			Namespace:   status.Namespace,
			IsLimited:   status.IsLimited,
			TriggeredBy: status.TriggeredBy,
			LimitResult: status.LimitResult,
			AppliedAt:   status.AppliedAt,
			LastCheckAt: status.LastCheckAt,
		}
		result[containerID] = statusCopy
	}
	return result
}

// GetContainerLimitStatus 获取单个容器的限速状态（API 接口）
func (m *SmartLimitManager) GetContainerLimitStatus(containerID string) (*LimitStatus, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status, exists := m.limitStatus[containerID]
	if !exists {
		return nil, false
	}

	// 创建副本以避免并发问题
	statusCopy := &LimitStatus{
		ContainerID: status.ContainerID,
		PodName:     status.PodName,
		Namespace:   status.Namespace,
		IsLimited:   status.IsLimited,
		TriggeredBy: status.TriggeredBy,
		LimitResult: status.LimitResult,
		AppliedAt:   status.AppliedAt,
		LastCheckAt: status.LastCheckAt,
	}
	return statusCopy, true
}

// GetAllContainerHistory 获取所有容器的历史数据（API 接口）
func (m *SmartLimitManager) GetAllContainerHistory() map[string]*ContainerIOHistory {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*ContainerIOHistory)
	for containerID, history := range m.history {
		// 创建副本以避免并发问题
		history.mu.RLock()
		historyCopy := &ContainerIOHistory{
			ContainerID: history.ContainerID,
			PodName:     history.PodName,
			Namespace:   history.Namespace,
			LastUpdate:  history.LastUpdate,
			Stats:       make([]*kubeclient.IOStats, len(history.Stats)),
		}
		copy(historyCopy.Stats, history.Stats)
		history.mu.RUnlock()
		result[containerID] = historyCopy
	}
	return result
}

// GetContainerHistory 获取单个容器的历史数据（API 接口）
func (m *SmartLimitManager) GetContainerHistory(containerID string) (*ContainerIOHistory, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	history, exists := m.history[containerID]
	if !exists {
		return nil, false
	}

	// 创建副本以避免并发问题
	history.mu.RLock()
	historyCopy := &ContainerIOHistory{
		ContainerID: history.ContainerID,
		PodName:     history.PodName,
		Namespace:   history.Namespace,
		LastUpdate:  history.LastUpdate,
		Stats:       make([]*kubeclient.IOStats, len(history.Stats)),
	}
	copy(historyCopy.Stats, history.Stats)
	history.mu.RUnlock()
	return historyCopy, true
}
