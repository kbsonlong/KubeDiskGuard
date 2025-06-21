package smartlimit

import (
	"testing"
	"time"

	"KubeDiskGuard/pkg/cgroup"
)

func TestCalculateIOTrend(t *testing.T) {
	config := &SmartLimitConfig{
		Enabled:           true,
		MonitorInterval:   60 * time.Second,
		HistoryWindow:     10 * time.Minute,
		HighIOThreshold:   1000,
		AutoLimitIOPS:     500,
		AutoLimitBPS:      1024 * 1024, // 1MB/s
		AnnotationPrefix:  "iops-limit",
		ExcludeNamespaces: []string{"kube-system"},
	}

	manager := &SmartLimitManager{
		config:  config,
		history: make(map[string]*ContainerIOHistory),
	}

	// 创建模拟的IO统计数据
	stats := []*cgroup.IOStats{
		{
			ContainerID: "test-container",
			Timestamp:   time.Now().Add(-5 * time.Minute),
			ReadIOPS:    100,
			WriteIOPS:   200,
			ReadBPS:     1024 * 1024,     // 1MB
			WriteBPS:    2 * 1024 * 1024, // 2MB
		},
		{
			ContainerID: "test-container",
			Timestamp:   time.Now().Add(-4 * time.Minute),
			ReadIOPS:    300,
			WriteIOPS:   400,
			ReadBPS:     3 * 1024 * 1024, // 3MB
			WriteBPS:    4 * 1024 * 1024, // 4MB
		},
		{
			ContainerID: "test-container",
			Timestamp:   time.Now().Add(-3 * time.Minute),
			ReadIOPS:    500,
			WriteIOPS:   600,
			ReadBPS:     5 * 1024 * 1024, // 5MB
			WriteBPS:    6 * 1024 * 1024, // 6MB
		},
	}

	trend := manager.calculateIOTrend(stats)

	// 验证趋势计算结果
	if trend.ReadIOPS15m <= 0 {
		t.Errorf("Expected positive ReadIOPS15m, got %f", trend.ReadIOPS15m)
	}
	if trend.WriteIOPS15m <= 0 {
		t.Errorf("Expected positive WriteIOPS15m, got %f", trend.WriteIOPS15m)
	}
	if trend.ReadBPS15m <= 0 {
		t.Errorf("Expected positive ReadBPS15m, got %f", trend.ReadBPS15m)
	}
	if trend.WriteBPS15m <= 0 {
		t.Errorf("Expected positive WriteBPS15m, got %f", trend.WriteBPS15m)
	}

	t.Logf("IO Trend: ReadIOPS15m=%f, WriteIOPS15m=%f, ReadBPS15m=%f, WriteBPS15m=%f",
		trend.ReadIOPS15m, trend.WriteIOPS15m, trend.ReadBPS15m, trend.WriteBPS15m)
}

func TestShouldApplyLimit(t *testing.T) {
	config := &SmartLimitConfig{
		Enabled:           true,
		HighIOThreshold:   1000,        // IOPS阈值
		HighBPSThreshold:  1024 * 1024, // BPS阈值（1MB/s）
		AutoLimitIOPS:     500,
		AutoLimitBPS:      1024 * 1024,
		AnnotationPrefix:  "iops-limit",
		ExcludeNamespaces: []string{"kube-system"},
	}

	manager := &SmartLimitManager{
		config: config,
	}

	// 测试高IO情况（IOPS超过阈值）
	highIOTrend := &IOTrend{
		ReadIOPS15m:  1500,
		WriteIOPS15m: 1600,
		ReadBPS15m:   2 * 1024 * 1024,
		WriteBPS15m:  2.5 * 1024 * 1024,
		ReadIOPS30m:  1400,
		WriteIOPS30m: 1500,
		ReadBPS30m:   1.8 * 1024 * 1024,
		WriteBPS30m:  2.2 * 1024 * 1024,
		ReadIOPS60m:  1300,
		WriteIOPS60m: 1400,
		ReadBPS60m:   1.6 * 1024 * 1024,
		WriteBPS60m:  2.0 * 1024 * 1024,
	}

	result := manager.shouldApplyLimit(highIOTrend)
	t.Logf("High IO trend result: %v", result)
	if !result {
		t.Error("Expected shouldApplyLimit to return true for high IO trend")
	}

	// 测试正常IO情况（IOPS和BPS都低于阈值）
	normalIOTrend := &IOTrend{
		ReadIOPS15m:  500,
		WriteIOPS15m: 600,
		ReadBPS15m:   512 * 1024,
		WriteBPS15m:  600 * 1024,
		ReadIOPS30m:  450,
		WriteIOPS30m: 550,
		ReadBPS30m:   480 * 1024,
		WriteBPS30m:  580 * 1024,
		ReadIOPS60m:  400,
		WriteIOPS60m: 500,
		ReadBPS60m:   450 * 1024,
		WriteBPS60m:  550 * 1024,
	}

	result = manager.shouldApplyLimit(normalIOTrend)
	t.Logf("Normal IO trend result: %v", result)
	if result {
		t.Error("Expected shouldApplyLimit to return false for normal IO trend")
	}
}

func TestCalculateLimitIOPS(t *testing.T) {
	config := &SmartLimitConfig{
		Enabled:           true,
		HighIOThreshold:   1000,
		AutoLimitIOPS:     500,
		AutoLimitBPS:      1024 * 1024,
		AnnotationPrefix:  "iops-limit",
		ExcludeNamespaces: []string{"kube-system"},
	}

	manager := &SmartLimitManager{
		config: config,
	}

	trend := &IOTrend{
		ReadIOPS15m:  1500,
		WriteIOPS15m: 1600,
		ReadIOPS30m:  1400,
		WriteIOPS30m: 1500,
		ReadIOPS60m:  1300,
		WriteIOPS60m: 1400,
	}

	limitIOPS := manager.calculateLimitIOPS(trend)

	// 验证限速值在合理范围内
	expectedMin := config.AutoLimitIOPS
	expectedMax := int((1500 + 1600) * 0.8) // 最高IOPS的80%

	if limitIOPS < expectedMin {
		t.Errorf("Expected limitIOPS >= %d, got %d", expectedMin, limitIOPS)
	}
	if limitIOPS > expectedMax {
		t.Errorf("Expected limitIOPS <= %d, got %d", expectedMax, limitIOPS)
	}

	t.Logf("Calculated limit IOPS: %d", limitIOPS)
}

func TestCalculateLimitBPS(t *testing.T) {
	config := &SmartLimitConfig{
		Enabled:           true,
		HighIOThreshold:   1000,
		AutoLimitIOPS:     500,
		AutoLimitBPS:      1024 * 1024,
		AnnotationPrefix:  "iops-limit",
		ExcludeNamespaces: []string{"kube-system"},
	}

	manager := &SmartLimitManager{
		config: config,
	}

	trend := &IOTrend{
		ReadBPS15m:  2 * 1024 * 1024,
		WriteBPS15m: 2.5 * 1024 * 1024,
		ReadBPS30m:  1.8 * 1024 * 1024,
		WriteBPS30m: 2.2 * 1024 * 1024,
		ReadBPS60m:  1.6 * 1024 * 1024,
		WriteBPS60m: 2.0 * 1024 * 1024,
	}

	limitBPS := manager.calculateLimitBPS(trend)

	// 验证限速值在合理范围内
	expectedMin := config.AutoLimitBPS
	expectedMax := int((2.5*1024*1024 + 2.5*1024*1024) * 0.8) // 最高BPS的80%

	if limitBPS < expectedMin {
		t.Errorf("Expected limitBPS >= %d, got %d", expectedMin, limitBPS)
	}
	if limitBPS > expectedMax {
		t.Errorf("Expected limitBPS <= %d, got %d", expectedMax, limitBPS)
	}

	t.Logf("Calculated limit BPS: %d", limitBPS)
}

func TestParseContainerID(t *testing.T) {
	tests := []struct {
		name     string
		k8sID    string
		expected string
	}{
		{
			name:     "docker container ID",
			k8sID:    "docker://abc123def456",
			expected: "abc123def456",
		},
		{
			name:     "containerd container ID",
			k8sID:    "containerd://xyz789uvw012",
			expected: "xyz789uvw012",
		},
		{
			name:     "plain container ID",
			k8sID:    "plain123id",
			expected: "plain123id",
		},
		{
			name:     "empty container ID",
			k8sID:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseContainerID(tt.k8sID)
			if result != tt.expected {
				t.Errorf("parseContainerID(%s) = %s, expected %s", tt.k8sID, result, tt.expected)
			}
		})
	}
}
