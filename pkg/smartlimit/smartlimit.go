package smartlimit

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

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

// WindowTrend 表示单个窗口的趋势，区分读写IOPS/BPS
// Duration: 窗口长度（分钟）
type WindowTrend struct {
	Duration  int
	ReadIOPS  float64
	WriteIOPS float64
	ReadBPS   float64
	WriteBPS  float64
}

type SmartLimitConfig struct {
	Enabled             bool
	MonitorInterval     int
	Windows             []config.WindowConfig
	RemoveThreshold     int
	RemoveDelay         int
	RemoveCheckInterval int
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
	trends := m.AnalyzeAllContainerTrends() // map[string][]WindowTrend
	m.ApplyLimitIfNeeded(trends)
}

// ApplyLimitIfNeeded 根据分析结果判断并执行限速（多窗口分级，纯 WindowTrend 版）
func (m *SmartLimitManager) ApplyLimitIfNeeded(trends map[string][]WindowTrend) {
	for containerID, windowTrends := range trends {
		m.applyLimitForContainer(containerID, windowTrends)
	}
}

// applyLimitForContainer 多窗口分级限速决策（合并限速与解除逻辑）
func (m *SmartLimitManager) applyLimitForContainer(containerID string, windowTrends []WindowTrend) {
	m.mu.RLock()
	history, exists := m.history[containerID]
	m.mu.RUnlock()
	if !exists || len(windowTrends) == 0 {
		return
	}

	prefix := m.config.SmartLimitAnnotationPrefix
	// 1. 优先级遍历，找到第一个命中阈值的窗口
	for idx, trend := range windowTrends {
		wcfg := m.config.SmartLimitWindows[idx]
		if trend.ReadIOPS > float64(wcfg.IOPSThreshold) || trend.WriteIOPS > float64(wcfg.IOPSThreshold) ||
			trend.ReadBPS > float64(wcfg.BPSThreshold) || trend.WriteBPS > float64(wcfg.BPSThreshold) {
			// 需要限速，下发注解
			readIOPS := int(trend.ReadIOPS * 0.8)
			writeIOPS := int(trend.WriteIOPS * 0.8)
			readBPS := int(trend.ReadBPS * 0.8)
			writeBPS := int(trend.WriteBPS * 0.8)
			pod, err := m.kubeClient.GetPod(history.Namespace, history.PodName)
			if err != nil {
				log.Printf("[SmartLimit] 获取Pod失败: %v", err)
				return
			}
			annotations := make(map[string]string)
			for k, v := range pod.Annotations {
				annotations[k] = v
			}
			annotations[prefix+"/read-iops-limit"] = strconv.Itoa(readIOPS)
			annotations[prefix+"/write-iops-limit"] = strconv.Itoa(writeIOPS)
			annotations[prefix+"/read-bps-limit"] = strconv.Itoa(readBPS)
			annotations[prefix+"/write-bps-limit"] = strconv.Itoa(writeBPS)
			annotations[prefix+"/triggered-by"] = fmt.Sprintf("%dmin", trend.Duration)
			annotations[prefix+"/trigger-reason"] = fmt.Sprintf("窗口%d分钟IO超阈值", trend.Duration)
			pod.Annotations = annotations
			_, err = m.kubeClient.UpdatePod(pod)
			if err != nil {
				log.Printf("[SmartLimit] 更新Pod注解失败: %v", err)
				return
			}
			eventMsg := fmt.Sprintf("已应用智能限速: ReadIOPS=%d, WriteIOPS=%d, ReadBPS=%d, WriteBPS=%d", readIOPS, writeIOPS, readBPS, writeBPS)
			err = m.kubeClient.CreateEvent(history.Namespace, history.PodName, "Warning", "SmartLimitApplied", eventMsg)
			if err != nil {
				log.Printf("[SmartLimit] 创建限速事件失败: %v", err)
			}
			log.Printf("[SmartLimit] Pod %s/%s 窗口%d分钟限速: ReadIOPS=%d, WriteIOPS=%d, ReadBPS=%d, WriteBPS=%d", history.PodName, history.Namespace, trend.Duration, readIOPS, writeIOPS, readBPS, writeBPS)
			m.mu.Lock()
			m.limitStatus[containerID] = &LimitStatus{
				ContainerID: containerID,
				PodName:     history.PodName,
				Namespace:   history.Namespace,
				IsLimited:   true,
				AppliedAt:   time.Now(),
				LastCheckAt: time.Now(),
			}
			m.mu.Unlock()
			return // 命中阈值，限速，直接返回
		}
	}
	// 2. 所有窗口都未命中阈值，尝试解除限速
	m.mu.Lock()
	status, limited := m.limitStatus[containerID]
	m.mu.Unlock()
	if limited && status.IsLimited {
		pod, err := m.kubeClient.GetPod(history.Namespace, history.PodName)
		if err != nil {
			log.Printf("[SmartLimit] 获取Pod失败: %v", err)
			return
		}
		annotations := make(map[string]string)
		for k, v := range pod.Annotations {
			if !strings.HasPrefix(k, prefix+"/") {
				annotations[k] = v
			}
		}
		annotations[prefix+"/limit-removed"] = "true"
		annotations[prefix+"/removed-at"] = time.Now().Format(time.RFC3339)
		annotations[prefix+"/removed-reason"] = "IO已恢复正常，解除限速"
		pod.Annotations = annotations
		_, err = m.kubeClient.UpdatePod(pod)
		if err != nil {
			log.Printf("[SmartLimit] 移除限速注解失败: %v", err)
			return
		}
		eventMsg := "已解除智能限速，IO已恢复正常"
		err = m.kubeClient.CreateEvent(history.Namespace, history.PodName, "Normal", "SmartLimitRemoved", eventMsg)
		if err != nil {
			log.Printf("[SmartLimit] 创建解除限速事件失败: %v", err)
		}
		log.Printf("[SmartLimit] Pod %s/%s 解除限速", history.PodName, history.Namespace)
		m.mu.Lock()
		delete(m.limitStatus, containerID)
		m.mu.Unlock()
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
