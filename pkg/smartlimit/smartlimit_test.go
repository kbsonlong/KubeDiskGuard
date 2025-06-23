package smartlimit

import (
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/kubeclient"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockKubeClient 是一个用于测试的模拟kubeClient
type mockKubeClient struct {
	kubeclient.IKubeClient
	pods []corev1.Pod
	mu   sync.Mutex
}

func (m *mockKubeClient) ListNodePodsWithKubeletFirst() ([]corev1.Pod, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pods, nil
}

func (m *mockKubeClient) GetPod(namespace, name string) (*corev1.Pod, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pod := range m.pods {
		if pod.Namespace == namespace && pod.Name == name {
			return &pod, nil
		}
	}
	return nil, fmt.Errorf("pod not found")
}

func (m *mockKubeClient) UpdatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, p := range m.pods {
		if p.Namespace == pod.Namespace && p.Name == pod.Name {
			m.pods[i] = *pod
			return pod, nil
		}
	}
	return nil, fmt.Errorf("pod not found")
}

func newTestManager(cfg *config.Config) *SmartLimitManager {
	return &SmartLimitManager{
		config:      cfg,
		history:     make(map[string]*ContainerIOHistory),
		limitStatus: make(map[string]*LimitStatus),
		stopCh:      make(chan struct{}),
		kubeClient:  &mockKubeClient{},
		// No cgroupMgr needed for these specific tests
	}
}

func TestCalculateIOTrend(t *testing.T) {
	manager := newTestManager(&config.Config{})

	now := time.Now()
	stats := []*kubeclient.IOStats{
		{Timestamp: now.Add(-2 * time.Minute), ReadIOPS: 100, WriteIOPS: 200, ReadBPS: 1000, WriteBPS: 2000},
		{Timestamp: now.Add(-1 * time.Minute), ReadIOPS: 160, WriteIOPS: 320, ReadBPS: 1600, WriteBPS: 3200}, // delta: 60, 120, 600, 1200 over 60s
	}

	trend := manager.calculateIOTrend(stats)

	// Expected: delta / 60 seconds
	expectedReadIOPS := float64(60) / 60.0
	expectedWriteIOPS := float64(120) / 60.0
	expectedReadBPS := float64(600) / 60.0
	expectedWriteBPS := float64(1200) / 60.0

	if math.Abs(trend.ReadIOPS15m-expectedReadIOPS) > 0.01 {
		t.Errorf("ReadIOPS15m wrong. got=%.2f, want=%.2f", trend.ReadIOPS15m, expectedReadIOPS)
	}
	if math.Abs(trend.WriteIOPS15m-expectedWriteIOPS) > 0.01 {
		t.Errorf("WriteIOPS15m wrong. got=%.2f, want=%.2f", trend.WriteIOPS15m, expectedWriteIOPS)
	}
	if math.Abs(trend.ReadBPS15m-expectedReadBPS) > 0.01 {
		t.Errorf("ReadBPS15m wrong. got=%.2f, want=%.2f", trend.ReadBPS15m, expectedReadBPS)
	}
	if math.Abs(trend.WriteBPS15m-expectedWriteBPS) > 0.01 {
		t.Errorf("WriteBPS15m wrong. got=%.2f, want=%.2f", trend.WriteBPS15m, expectedWriteBPS)
	}
}

func TestShouldApplyLimit(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.SmartLimitGradedThresholds = true
	cfg.SmartLimitIOThreshold15m = 100
	cfg.SmartLimitIOPSLimit15m = 115
	cfg.SmartLimitIOThreshold30m = 200
	cfg.SmartLimitIOPSLimit30m = 230
	cfg.SmartLimitIOThreshold60m = 300
	cfg.SmartLimitIOPSLimit60m = 360

	manager := newTestManager(cfg)

	tests := []struct {
		name            string
		trend           *IOTrend
		expectLimit     bool
		expectedIOPS    int
		expectedTrigger string
	}{
		{"NoLimit", &IOTrend{}, false, 0, ""},
		{"15mTrigger", &IOTrend{ReadIOPS15m: 150}, true, 115, "15m"},
		{"30mTrigger", &IOTrend{ReadIOPS15m: 50, ReadIOPS30m: 250}, true, 230, "30m"},
		{"60mTrigger", &IOTrend{ReadIOPS15m: 50, ReadIOPS30m: 150, ReadIOPS60m: 350}, true, 360, "60m"},
		{"15mHasPriority", &IOTrend{ReadIOPS15m: 150, ReadIOPS30m: 250}, true, 115, "15m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldLimit, result := manager.shouldApplyLimit(tt.trend)
			if shouldLimit != tt.expectLimit {
				t.Errorf("shouldLimit mismatch. got=%v, want=%v", shouldLimit, tt.expectLimit)
			}
			if shouldLimit {
				if result.ReadIOPS != tt.expectedIOPS {
					t.Errorf("expectedIOPS mismatch. got=%d, want=%d", result.ReadIOPS, tt.expectedIOPS)
				}
				if result.TriggeredBy != tt.expectedTrigger {
					t.Errorf("expectedTrigger mismatch. got=%s, want=%s", result.TriggeredBy, tt.expectedTrigger)
				}
			}
		})
	}

	// Test legacy mode
	cfg.SmartLimitGradedThresholds = false
	cfg.SmartLimitHighIOThreshold = 100
	manager.config = cfg
	shouldLimit, _ := manager.shouldApplyLimit(&IOTrend{ReadIOPS15m: 150})
	if !shouldLimit {
		t.Error("shouldApplyLimit failed in legacy mode")
	}
}

func TestShouldRemoveLimit(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.SmartLimitRemoveThreshold = 50
	cfg.SmartLimitRemoveDelay = 5         // 5 minutes
	cfg.SmartLimitRemoveCheckInterval = 1 // 1 minute
	manager := newTestManager(cfg)

	now := time.Now()

	status := &LimitStatus{
		IsLimited:   true,
		TriggeredBy: "15m",
		AppliedAt:   now.Add(-10 * time.Minute), // Applied 10 mins ago
		LastCheckAt: now.Add(-2 * time.Minute),  // Last checked 2 mins ago
	}

	tests := []struct {
		name         string
		trend        *IOTrend
		status       *LimitStatus
		expectRemove bool
	}{
		{"IOHigh", &IOTrend{ReadIOPS15m: 60}, status, false},
		{"IOLow", &IOTrend{ReadIOPS15m: 40}, status, true},
		{"InDelay", &IOTrend{ReadIOPS15m: 40}, &LimitStatus{AppliedAt: now.Add(-3 * time.Minute), LastCheckAt: now.Add(-2 * time.Minute), TriggeredBy: "15m"}, false},
		{"InCheckInterval", &IOTrend{ReadIOPS15m: 40}, &LimitStatus{AppliedAt: now.Add(-10 * time.Minute), LastCheckAt: now.Add(-30 * time.Second), TriggeredBy: "15m"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if remove := manager.shouldRemoveLimit(tt.trend, tt.status); remove != tt.expectRemove {
				t.Errorf("shouldRemoveLimit mismatch. got=%v, want=%v", remove, tt.expectRemove)
			}
		})
	}
}

func TestRestoreLimitStatus(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.SmartLimitAnnotationPrefix = "test.prefix.io"
	manager := newTestManager(cfg)

	prefix := cfg.SmartLimitAnnotationPrefix + "/"
	mockClient := &mockKubeClient{
		pods: []corev1.Pod{
			{ // Pod that should be restored
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-to-restore",
					Namespace: "default",
					Annotations: map[string]string{
						prefix + "triggered-by":    "15m",
						prefix + "read-iops-limit": "100",
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{ContainerID: "docker://container1"},
					},
				},
			},
			{ // Pod that was limited but now removed
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-removed",
					Namespace: "default",
					Annotations: map[string]string{
						prefix + "triggered-by":  "30m",
						prefix + "limit-removed": "true",
					},
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{ContainerID: "docker://container2"},
					},
				},
			},
			{ // Pod with no limit annotations
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-no-limit",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{ContainerID: "docker://container3"},
					},
				},
			},
		},
	}
	manager.kubeClient = mockClient

	manager.restoreLimitStatus()

	if _, exists := manager.limitStatus["container2"]; exists {
		t.Error("container2 with removed limit should not be in limitStatus")
	}
	if _, exists := manager.limitStatus["container3"]; exists {
		t.Error("container3 with no limit should not be in limitStatus")
	}

	status, exists := manager.limitStatus["container1"]
	if !exists {
		t.Fatal("container1 should be in limitStatus")
	}
	if !status.IsLimited {
		t.Error("container1 status IsLimited should be true")
	}
	if status.LimitResult.TriggeredBy != "15m" {
		t.Errorf("container1 TriggeredBy mismatch. got=%s, want=15m", status.LimitResult.TriggeredBy)
	}
	if status.LimitResult.ReadIOPS != 100 {
		t.Errorf("container1 ReadIOPS mismatch. got=%d, want=100", status.LimitResult.ReadIOPS)
	}
}

func TestShouldUpdateLimit(t *testing.T) {
	manager := newTestManager(&config.Config{})

	currentStatus := &LimitStatus{
		IsLimited:   true,
		TriggeredBy: "15m",
		LimitResult: &LimitResult{
			TriggeredBy: "15m",
			ReadIOPS:    100,
		},
	}

	tests := []struct {
		name         string
		newResult    *LimitResult
		expectUpdate bool
	}{
		{"NoChange", &LimitResult{TriggeredBy: "15m", ReadIOPS: 100}, false},
		{"IOPSChange", &LimitResult{TriggeredBy: "15m", ReadIOPS: 200}, true},
		{"TriggerChange", &LimitResult{TriggeredBy: "30m", ReadIOPS: 100}, true},
		{"BothChange", &LimitResult{TriggeredBy: "30m", ReadIOPS: 200}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if update := manager.shouldUpdateLimit(currentStatus, tt.newResult); update != tt.expectUpdate {
				t.Errorf("shouldUpdateLimit mismatch. got=%v, want=%v", update, tt.expectUpdate)
			}
		})
	}
}

func TestParseContainerID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"docker", "docker://abc", "abc"},
		{"containerd", "containerd://def", "def"},
		{"plain", "ghi", "ghi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseContainerID(tt.input); got != tt.expected {
				t.Errorf("parseContainerID() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShouldMonitorPodByNamespace(t *testing.T) {
	cfg := &config.Config{ExcludeNamespaces: []string{"kube-system"}}
	manager := newTestManager(cfg)

	if manager.shouldMonitorPodByNamespace("kube-system") {
		t.Error("should not monitor kube-system")
	}
	if !manager.shouldMonitorPodByNamespace("default") {
		t.Error("should monitor default")
	}
}
