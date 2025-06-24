package smartlimit

import (
	"KubeDiskGuard/pkg/kubeclient"
	"log"
	"time"
)

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
