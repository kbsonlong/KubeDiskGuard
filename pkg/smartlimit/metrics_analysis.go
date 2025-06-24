package smartlimit

import (
	"KubeDiskGuard/pkg/kubeclient"
	"time"
)

// AnalyzeAllContainerTrends 分析所有容器的IO趋势，返回map
func (m *SmartLimitManager) AnalyzeAllContainerTrends() map[string]*IOTrend {
	m.mu.RLock()
	containers := make([]string, 0, len(m.history))
	for containerID := range m.history {
		containers = append(containers, containerID)
	}
	m.mu.RUnlock()

	trends := make(map[string]*IOTrend)
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
		trends[containerID] = m.AnalyzeContainerTrend(stats)
	}
	return trends
}

// AnalyzeContainerTrend 只负责分析并返回IO趋势
func (m *SmartLimitManager) AnalyzeContainerTrend(stats []*kubeclient.IOStats) *IOTrend {
	trend := &IOTrend{}
	if len(stats) < 2 {
		return trend
	}

	now := time.Now()
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
