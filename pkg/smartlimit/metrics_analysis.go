package smartlimit

import (
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/kubeclient"
	"fmt"
	"log"
	"time"
)

// AnalyzeAllContainerTrends 分析所有容器的IO趋势，返回map
func (m *SmartLimitManager) AnalyzeAllContainerTrends() map[string][]WindowTrend {
	m.mu.RLock()
	containers := make([]string, 0, len(m.history))
	for containerID := range m.history {
		containers = append(containers, containerID)
	}
	cfgWindows := m.config.SmartLimitWindows
	m.mu.RUnlock()

	trends := make(map[string][]WindowTrend)
	for _, containerID := range containers {
		m.mu.RLock()
		history, exists := m.history[containerID]
		m.mu.RUnlock()
		if !exists {
			continue
		}
		history.mu.RLock()
		stats := make([]*kubeclient.IOStats, len(history.Stats))
		copy(stats, history.Stats)
		history.mu.RUnlock()
		trends[containerID] = AnalyzeTrendsUniversal(stats, cfgWindows)
	}
	return trends
}

// AnalyzeTrends 计算每个窗口的平均IOPS/BPS
func AnalyzeTrends(stats []*kubeclient.IOStats, windows []config.WindowConfig) []WindowTrend {
	return AnalyzeTrendsUniversal(stats, windows)
}

// AnalyzeTrendsUniversal 支持任意自定义时间窗口的趋势分析，区分读写
func AnalyzeTrendsUniversal(stats []*kubeclient.IOStats, windows []config.WindowConfig) []WindowTrend {
	now := time.Now()
	trends := make([]WindowTrend, 0, len(windows))
	for _, w := range windows {
		cutoff := now.Add(-time.Duration(w.Duration) * time.Minute)
		var totalReadIOPS, totalWriteIOPS, totalReadBPS, totalWriteBPS float64
		var count int
		for i := 1; i < len(stats); i++ {
			if stats[i].Timestamp.After(cutoff) {
				readIOPS := stats[i].ReadIOPS - stats[i-1].ReadIOPS
				writeIOPS := stats[i].WriteIOPS - stats[i-1].WriteIOPS
				readBPS := stats[i].ReadBPS - stats[i-1].ReadBPS
				writeBPS := stats[i].WriteBPS - stats[i-1].WriteBPS
				timeDiff := stats[i].Timestamp.Sub(stats[i-1].Timestamp).Seconds()
				if timeDiff > 0 {
					totalReadIOPS += float64(readIOPS) / timeDiff
					totalWriteIOPS += float64(writeIOPS) / timeDiff
					totalReadBPS += float64(readBPS) / timeDiff
					totalWriteBPS += float64(writeBPS) / timeDiff
					count++
				}
			}
		}
		avgReadIOPS, avgWriteIOPS, avgReadBPS, avgWriteBPS := 0.0, 0.0, 0.0, 0.0
		if count > 0 {
			avgReadIOPS = totalReadIOPS / float64(count)
			avgWriteIOPS = totalWriteIOPS / float64(count)
			avgReadBPS = totalReadBPS / float64(count)
			avgWriteBPS = totalWriteBPS / float64(count)
		}
		trends = append(trends, WindowTrend{
			Duration:  w.Duration,
			ReadIOPS:  avgReadIOPS,
			WriteIOPS: avgWriteIOPS,
			ReadBPS:   avgReadBPS,
			WriteBPS:  avgWriteBPS,
		})
	}
	return trends
}

// ShouldLimit 判断是否需要限速
func ShouldLimit(trends []WindowTrend, config SmartLimitConfig) (bool, int, string, string) {
	for idx, w := range config.Windows {
		if trends[idx].ReadIOPS > float64(w.IOPSThreshold) {
			return true, idx, "ReadIOPS", fmt.Sprintf("窗口%d分钟 ReadIOPS %.2f > 阈值%d", w.Duration, trends[idx].ReadIOPS, w.IOPSThreshold)
		}
		if trends[idx].WriteIOPS > float64(w.IOPSThreshold) {
			return true, idx, "WriteIOPS", fmt.Sprintf("窗口%d分钟 WriteIOPS %.2f > 阈值%d", w.Duration, trends[idx].WriteIOPS, w.IOPSThreshold)
		}
		if trends[idx].ReadBPS > float64(w.BPSThreshold) {
			return true, idx, "ReadBPS", fmt.Sprintf("窗口%d分钟 ReadBPS %.2f > 阈值%d", w.Duration, trends[idx].ReadBPS, w.BPSThreshold)
		}
		if trends[idx].WriteBPS > float64(w.BPSThreshold) {
			return true, idx, "WriteBPS", fmt.Sprintf("窗口%d分钟 WriteBPS %.2f > 阈值%d", w.Duration, trends[idx].WriteBPS, w.BPSThreshold)
		}
	}
	return false, -1, "", ""
}

// ShouldRemoveLimit 判断是否可以解除限速
func ShouldRemoveLimit(trends []WindowTrend, config SmartLimitConfig, lastLimitTime, lastCheckTime time.Time) bool {
	for _, t := range trends {
		if t.ReadIOPS > float64(config.RemoveThreshold) || t.WriteIOPS > float64(config.RemoveThreshold) ||
			t.ReadBPS > float64(config.RemoveThreshold) || t.WriteBPS > float64(config.RemoveThreshold) {
			return false
		}
	}
	if time.Since(lastLimitTime) < time.Duration(config.RemoveDelay)*time.Minute {
		return false
	}
	if time.Since(lastCheckTime) < time.Duration(config.RemoveCheckInterval)*time.Minute {
		return false
	}
	return true
}

// ApplyLimit 执行限速（实际应下发注解）
func ApplyLimit(containerID string, window WindowTrend, config SmartLimitConfig) {
	log.Printf("[SmartLimit] 对容器%s应用限速: %d分钟窗口, ReadIOPS限速=%.0f, WriteIOPS限速=%.0f, ReadBPS限速=%.0f, WriteBPS限速=%.0f\n",
		containerID, window.Duration, window.ReadIOPS*0.8, window.WriteIOPS*0.8, window.ReadBPS*0.8, window.WriteBPS*0.8)
}

// RemoveLimit 解除限速（实际应移除注解）
func RemoveLimit(containerID string, config SmartLimitConfig) {
	log.Printf("[SmartLimit] 对容器%s解除限速\n", containerID)
}

// EmitLimitEvent 输出限速事件
func EmitLimitEvent(containerID, namespace, podName, reason string) {
	log.Printf("[Event] 容器%s(%s/%s)限速: %s\n", containerID, namespace, podName, reason)
}
