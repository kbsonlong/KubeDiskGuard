package smartlimit

import (
	"fmt"
	"sync"
	"testing"

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

func (m *mockKubeClient) CreateEvent(namespace, podName, eventType, reason, message string) error {
	return nil
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
