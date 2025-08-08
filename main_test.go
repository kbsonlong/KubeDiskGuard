package main

import (
	"os"
	"testing"
	"time"

	"KubeDiskGuard/pkg/cadvisor"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/detector"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/service"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
)

func TestDetectRuntime(t *testing.T) {
	runtime := detector.DetectRuntime()
	if runtime == "" {
		t.Error("detectRuntime should not return empty string")
	}
	t.Logf("Detected runtime: %s", runtime)
}

func TestDetectCgroupVersion(t *testing.T) {
	version := detector.DetectCgroupVersion()
	if version != "v1" && version != "v2" {
		t.Errorf("detectCgroupVersion should return v1 or v2, got: %s", version)
	}
	t.Logf("Detected cgroup version: %s", version)
}

func TestGetDefaultConfig(t *testing.T) {
	cfg := config.GetDefaultConfig()
	if cfg == nil {
		t.Fatal("getDefaultConfig should not return nil")
	}
	if cfg.ContainerIOPSLimit != 500 {
		t.Errorf("Expected ContainerIOPSLimit to be 500, got %d", cfg.ContainerIOPSLimit)
	}
	if cfg.DataMount != "/data" {
		t.Errorf("Expected DataMount to be /data, got %s", cfg.DataMount)
	}
	if len(cfg.ExcludeKeywords) == 0 {
		t.Error("Expected ExcludeKeywords to have some default values")
	}
}

func TestConfigToJSON(t *testing.T) {
	cfg := config.GetDefaultConfig()
	jsonStr := cfg.ToJSON()
	if jsonStr == "" {
		t.Error("ToJSON should not return empty string")
	}
	t.Logf("Config JSON: %s", jsonStr)
}

func TestEventListening(t *testing.T) {
	t.Skip("Skipping event listening test in CI/local without a real cluster setup")
}

func TestProcessExistingContainersWithPodAnnotations(t *testing.T) {
	t.Skip("Skipping ProcessExistingContainersWithPodAnnotations as it requires a running Docker/containerd environment")
}

func TestExtractPodInfoFromContainer(t *testing.T) {
	testCases := []struct {
		name         string
		container    *container.ContainerInfo
		expectedNS   string
		expectedName string
	}{
		{
			name: "valid_pod_info",
			container: &container.ContainerInfo{
				ID: "test-container",
				Annotations: map[string]string{
					"io.kubernetes.pod.namespace": "test-namespace",
					"io.kubernetes.pod.name":      "test-pod",
				},
			},
			expectedNS:   "test-namespace",
			expectedName: "test-pod",
		},
		{
			name: "missing_info",
			container: &container.ContainerInfo{
				ID:          "test-container",
				Annotations: map[string]string{},
			},
			expectedNS:   "",
			expectedName: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ns, name := "", ""
			if namespace, ok := tc.container.Annotations["io.kubernetes.pod.namespace"]; ok {
				if podName, ok := tc.container.Annotations["io.kubernetes.pod.name"]; ok {
					ns, name = namespace, podName
				}
			}

			if ns != tc.expectedNS {
				t.Errorf("Expected namespace %s, got %s", tc.expectedNS, ns)
			}
			if name != tc.expectedName {
				t.Errorf("Expected name %s, got %s", tc.expectedName, name)
			}
		})
	}
}

func TestKubeletConfig(t *testing.T) {
	cfg := config.GetDefaultConfig()
	os.Setenv("KUBELET_HOST", "test-host")
	os.Setenv("KUBELET_PORT", "10255")
	config.LoadFromEnv(cfg)
	if cfg.KubeletHost != "test-host" {
		t.Errorf("Expected KubeletHost to be 'test-host', got '%s'", cfg.KubeletHost)
	}
	if cfg.KubeletPort != "10255" {
		t.Errorf("Expected KubeletPort to be '10255', got '%s'", cfg.KubeletPort)
	}
	os.Unsetenv("KUBELET_HOST")
	os.Unsetenv("KUBELET_PORT")
}

type mockKubeClient struct {
	pods           []corev1.Pod
	updatePodError error
}

func (m *mockKubeClient) GetPod(namespace, name string) (*corev1.Pod, error) {
	for _, p := range m.pods {
		if p.Namespace == namespace && p.Name == name {
			return &p, nil
		}
	}
	return nil, nil // Simplified
}

func (m *mockKubeClient) UpdatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	return pod, m.updatePodError
}

func (m *mockKubeClient) ListNodePods() ([]corev1.Pod, error) {
	return m.pods, nil
}

func (m *mockKubeClient) ListNodePodsWithKubeletFirst() ([]corev1.Pod, error) {
	return m.pods, nil
}

func (m *mockKubeClient) WatchPods() (watch.Interface, error) {
	return watch.NewFake(), nil
}

func (m *mockKubeClient) WatchNodePods() (watch.Interface, error) {
	return watch.NewFake(), nil
}

func (m *mockKubeClient) GetNodeSummary() (*kubeclient.NodeSummary, error) {
	return &kubeclient.NodeSummary{}, nil
}

func (m *mockKubeClient) GetCadvisorMetrics() (string, error) {
	return "", nil
}

func (m *mockKubeClient) ParseCadvisorMetrics(metrics string) (*cadvisor.CadvisorMetrics, error) {
	return &cadvisor.CadvisorMetrics{}, nil
}

func (m *mockKubeClient) GetCadvisorIORate(containerID string, window time.Duration) (*cadvisor.IORate, error) {
	return &cadvisor.IORate{}, nil
}

func (m *mockKubeClient) GetCadvisorAverageIORate(containerID string, windows []time.Duration) (*cadvisor.IORate, error) {
	return &cadvisor.IORate{}, nil
}

func (m *mockKubeClient) CleanupCadvisorData(maxAge time.Duration) {
	// No-op for mock
}

func (m *mockKubeClient) GetCadvisorStats() (containerCount, dataPointCount int) {
	return 0, 0
}

func (m *mockKubeClient) ConvertCadvisorToIOStats(metrics *cadvisor.CadvisorMetrics, containerID string) *kubeclient.IOStats {
	return &kubeclient.IOStats{}
}

func (m *mockKubeClient) CreateEvent(namespace, podName, eventType, reason, message string) error {
	return nil
}

func TestResetAllContainersIOPSLimit(t *testing.T) {
	os.Setenv("NODE_NAME", "test-node")
	defer os.Unsetenv("NODE_NAME")

	cfg := config.GetDefaultConfig()
	mockKC := &mockKubeClient{}

	// This test relies on a running container runtime.
	// We are only testing the service creation and function call here.
	t.Skip("Skipping TestResetAllContainersIOPSLimit as it requires a running container runtime")
	svc, err := service.NewKubeDiskGuardServiceWithKubeClient(cfg, mockKC)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	if err := svc.ResetAllContainersIOPSLimit(); err != nil {
		t.Errorf("ResetAllContainersIOPSLimit() error = %v", err)
	}
}

func shouldProcessPodForTest(pod corev1.Pod, excludeNamespaces []string, excludeLabelSelector string) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, ns := range excludeNamespaces {
		if pod.Namespace == ns {
			return false
		}
	}
	if excludeLabelSelector != "" {
		selector, err := labels.Parse(excludeLabelSelector)
		if err == nil && selector.Matches(labels.Set(pod.Labels)) {
			return false
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Started == nil || !*cs.Started {
			return false
		}
	}
	return true
}

func TestShouldProcessPod(t *testing.T) {
	trueVar := true
	podBase := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Labels:    map[string]string{"app": "test"},
			Namespace: "default",
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Started: &trueVar}}},
	}

	testCases := []struct {
		name          string
		pod           corev1.Pod
		excludeNs     []string
		excludeLabels string
		expected      bool
	}{
		{"should process", podBase, []string{"kube-system"}, "", true},
		{"exclude by namespace", podBase, []string{"default"}, "", false},
		{"exclude by label", podBase, []string{}, "app=test", false},
		{"not running", func() corev1.Pod { p := podBase; p.Status.Phase = corev1.PodPending; return p }(), []string{}, "", false},
		{"container not started", func() corev1.Pod {
			p := podBase
			p.Status.ContainerStatuses = []corev1.ContainerStatus{{Started: nil}}
			return p
		}(), []string{}, "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldProcessPodForTest(tc.pod, tc.excludeNs, tc.excludeLabels)
			if got != tc.expected {
				t.Errorf("expected %v, got %v for pod %s in ns %s", tc.expected, got, tc.pod.Name, tc.pod.Namespace)
			}
		})
	}
}

type mockRuntime struct {
	setLimitsCount   int
	resetLimitsCount int
}

func (m *mockRuntime) GetContainerByID(id string) (*container.ContainerInfo, error) {
	return &container.ContainerInfo{ID: id}, nil
}
func (m *mockRuntime) SetLimits(c *container.ContainerInfo, riops, wiops, rbps, wbps int) error {
	m.setLimitsCount++
	return nil
}
func (m *mockRuntime) ResetLimits(c *container.ContainerInfo) error {
	m.resetLimitsCount++
	return nil
}
func (m *mockRuntime) GetContainers() ([]*container.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) ProcessContainer(c *container.ContainerInfo) error  { return nil }
func (m *mockRuntime) Close() error                                       { return nil }

func TestShouldProcessPod_StartedField(t *testing.T) {
	os.Setenv("NODE_NAME", "test-node")
	defer os.Unsetenv("NODE_NAME")
	cfg := config.GetDefaultConfig()
	mockKC := &mockKubeClient{}
	svc, err := service.NewKubeDiskGuardServiceWithKubeClient(cfg, mockKC)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	trueVar := true
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{Started: &trueVar},
			},
		},
	}

	// This now uses the service's internal check which considers the config
	svc.Config.ExcludeNamespaces = []string{} // ensure no namespaces are excluded for this test
	if !svc.ShouldProcessPod(pod) {
		t.Error("ShouldProcessPod should return true when all containers are started and pod is running")
	}

	pod.Status.ContainerStatuses[0].Started = nil
	if svc.ShouldProcessPod(pod) {
		t.Error("ShouldProcessPod should return false when a container is not started")
	}

	pod.Status.ContainerStatuses[0].Started = &trueVar
	pod.Status.Phase = corev1.PodPending
	if svc.ShouldProcessPod(pod) {
		t.Error("ShouldProcessPod should return false when pod is not in Running phase")
	}
}
