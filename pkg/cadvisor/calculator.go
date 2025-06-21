package cadvisor

import (
	"fmt"
	"sync"
	"time"
)

// MetricPoint 指标数据点
type MetricPoint struct {
	ContainerID string
	Timestamp   time.Time
	ReadIOPS    float64 // 累积读取次数
	WriteIOPS   float64 // 累积写入次数
	ReadBytes   float64 // 累积读取字节数
	WriteBytes  float64 // 累积写入字节数
}

// IORate 计算出的IO速率
type IORate struct {
	ContainerID string
	Timestamp   time.Time
	ReadIOPS    float64 // 每秒读取操作数
	WriteIOPS   float64 // 每秒写入操作数
	ReadBPS     float64 // 每秒读取字节数
	WriteBPS    float64 // 每秒写入字节数
}

// Calculator cAdvisor指标计算器
type Calculator struct {
	history map[string][]MetricPoint
	mu      sync.RWMutex
}

// NewCalculator 创建新的计算器
func NewCalculator() *Calculator {
	return &Calculator{
		history: make(map[string][]MetricPoint),
	}
}

// AddMetricPoint 添加指标数据点
func (c *Calculator) AddMetricPoint(containerID string, timestamp time.Time, readIOPS, writeIOPS, readBytes, writeBytes float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	point := MetricPoint{
		ContainerID: containerID,
		Timestamp:   timestamp,
		ReadIOPS:    readIOPS,
		WriteIOPS:   writeIOPS,
		ReadBytes:   readBytes,
		WriteBytes:  writeBytes,
	}

	if _, exists := c.history[containerID]; !exists {
		c.history[containerID] = make([]MetricPoint, 0)
	}

	c.history[containerID] = append(c.history[containerID], point)

	// 保持历史数据在合理范围内（最多保留100个数据点）
	if len(c.history[containerID]) > 100 {
		c.history[containerID] = c.history[containerID][len(c.history[containerID])-100:]
	}
}

// CalculateIORate 计算IO速率
func (c *Calculator) CalculateIORate(containerID string, window time.Duration) (*IORate, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	points, exists := c.history[containerID]
	if !exists || len(points) < 2 {
		return nil, fmt.Errorf("insufficient data points for container %s", containerID)
	}

	// 找到窗口内的数据点
	cutoff := time.Now().Add(-window)
	var windowPoints []MetricPoint

	for _, point := range points {
		if point.Timestamp.After(cutoff) {
			windowPoints = append(windowPoints, point)
		}
	}

	if len(windowPoints) < 2 {
		return nil, fmt.Errorf("insufficient data points in window for container %s", containerID)
	}

	// 计算增量
	latest := windowPoints[len(windowPoints)-1]
	earliest := windowPoints[0]

	timeDiff := latest.Timestamp.Sub(earliest.Timestamp).Seconds()
	if timeDiff <= 0 {
		return nil, fmt.Errorf("invalid time difference for container %s", containerID)
	}

	// 计算IOPS（每秒操作数）
	readIOPSDelta := latest.ReadIOPS - earliest.ReadIOPS
	writeIOPSDelta := latest.WriteIOPS - earliest.WriteIOPS
	readIOPS := readIOPSDelta / timeDiff
	writeIOPS := writeIOPSDelta / timeDiff

	// 计算BPS（每秒字节数）
	readBytesDelta := latest.ReadBytes - earliest.ReadBytes
	writeBytesDelta := latest.WriteBytes - earliest.WriteBytes
	readBPS := readBytesDelta / timeDiff
	writeBPS := writeBytesDelta / timeDiff

	return &IORate{
		ContainerID: containerID,
		Timestamp:   latest.Timestamp,
		ReadIOPS:    readIOPS,
		WriteIOPS:   writeIOPS,
		ReadBPS:     readBPS,
		WriteBPS:    writeBPS,
	}, nil
}

// CalculateAverageIORate 计算平均IO速率（使用多个时间窗口）
func (c *Calculator) CalculateAverageIORate(containerID string, windows []time.Duration) (*IORate, error) {
	var totalReadIOPS, totalWriteIOPS, totalReadBPS, totalWriteBPS float64
	var validCount int

	for _, window := range windows {
		rate, err := c.CalculateIORate(containerID, window)
		if err != nil {
			continue // 跳过无效窗口
		}

		totalReadIOPS += rate.ReadIOPS
		totalWriteIOPS += rate.WriteIOPS
		totalReadBPS += rate.ReadBPS
		totalWriteBPS += rate.WriteBPS
		validCount++
	}

	if validCount == 0 {
		return nil, fmt.Errorf("no valid data for container %s", containerID)
	}

	// 计算平均值
	avgReadIOPS := totalReadIOPS / float64(validCount)
	avgWriteIOPS := totalWriteIOPS / float64(validCount)
	avgReadBPS := totalReadBPS / float64(validCount)
	avgWriteBPS := totalWriteBPS / float64(validCount)

	return &IORate{
		ContainerID: containerID,
		Timestamp:   time.Now(),
		ReadIOPS:    avgReadIOPS,
		WriteIOPS:   avgWriteIOPS,
		ReadBPS:     avgReadBPS,
		WriteBPS:    avgWriteBPS,
	}, nil
}

// GetContainerHistory 获取容器的历史数据
func (c *Calculator) GetContainerHistory(containerID string) []MetricPoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if points, exists := c.history[containerID]; exists {
		// 返回副本
		result := make([]MetricPoint, len(points))
		copy(result, points)
		return result
	}
	return nil
}

// CleanupOldData 清理过期数据
func (c *Calculator) CleanupOldData(maxAge time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)

	for containerID, points := range c.history {
		var validPoints []MetricPoint
		for _, point := range points {
			if point.Timestamp.After(cutoff) {
				validPoints = append(validPoints, point)
			}
		}

		if len(validPoints) == 0 {
			delete(c.history, containerID)
		} else {
			c.history[containerID] = validPoints
		}
	}
}

// GetContainerCount 获取容器数量
func (c *Calculator) GetContainerCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.history)
}

// GetTotalDataPoints 获取总数据点数
func (c *Calculator) GetTotalDataPoints() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := 0
	for _, points := range c.history {
		total += len(points)
	}
	return total
}
