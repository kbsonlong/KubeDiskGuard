package smartlimit

import (
	"testing"
	"time"

	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/kubelet"
)

func TestCalculateIOTrend(t *testing.T) {
	cfg := &config.Config{
		SmartLimitEnabled:          true,
		SmartLimitMonitorInterval:  60,
		SmartLimitHistoryWindow:    10,
		SmartLimitHighIOThreshold:  1000,
		SmartLimitAutoIOPS:         500,
		SmartLimitAutoBPS:          1024 * 1024, // 1MB/s
		SmartLimitAnnotationPrefix: "io-limit",
		ExcludeNamespaces:          []string{"kube-system"},
	}

	manager := &SmartLimitManager{
		config:  cfg,
		history: make(map[string]*ContainerIOHistory),
	}

	// 创建模拟的IO统计数据
	stats := []*kubelet.IOStats{
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
	cfg := &config.Config{
		SmartLimitEnabled:          true,
		SmartLimitHighIOThreshold:  1000,        // IOPS阈值
		SmartLimitHighBPSThreshold: 1024 * 1024, // BPS阈值（1MB/s）
		SmartLimitAutoIOPS:         500,
		SmartLimitAutoBPS:          1024 * 1024,
		SmartLimitAnnotationPrefix: "io-limit",
		ExcludeNamespaces:          []string{"kube-system"},
	}

	manager := &SmartLimitManager{
		config: cfg,
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

func TestParseContainerID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "docker container ID",
			input:    "docker://1234567890abcdef",
			expected: "1234567890abcdef",
		},
		{
			name:     "containerd container ID",
			input:    "containerd://abcdef1234567890",
			expected: "abcdef1234567890",
		},
		{
			name:     "plain container ID",
			input:    "1234567890abcdef",
			expected: "1234567890abcdef",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseContainerID(tt.input)
			if result != tt.expected {
				t.Errorf("parseContainerID(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShouldMonitorPodByNamespace(t *testing.T) {
	cfg := &config.Config{
		ExcludeNamespaces: []string{"kube-system", "default"},
	}

	manager := &SmartLimitManager{
		config: cfg,
	}

	tests := []struct {
		name      string
		namespace string
		expected  bool
	}{
		{
			name:      "excluded namespace kube-system",
			namespace: "kube-system",
			expected:  false,
		},
		{
			name:      "excluded namespace default",
			namespace: "default",
			expected:  false,
		},
		{
			name:      "included namespace",
			namespace: "my-app",
			expected:  true,
		},
		{
			name:      "empty namespace",
			namespace: "",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.shouldMonitorPodByNamespace(tt.namespace)
			if result != tt.expected {
				t.Errorf("shouldMonitorPodByNamespace(%s) = %v, want %v", tt.namespace, result, tt.expected)
			}
		})
	}
}
