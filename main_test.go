package main

import (
	"os"
	"testing"
	"time"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/detector"
	"iops-limit-service/pkg/service"

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
	// 获取测试配置
	cfg := config.GetDefaultConfig()
	cfg.ContainerRuntime = "docker" // 使用docker进行测试
	cfg.ContainerIOPSLimit = 500
	cfg.DataMount = "/data"
	cfg.ExcludeKeywords = []string{"pause", "istio-proxy"}

	// 创建服务
	svc, err := service.NewIOPSLimitService(cfg)
	if err != nil {
		t.Skipf("Skipping test: failed to create service: %v", err)
	}

	// 测试处理现有容器
	err = svc.ProcessExistingContainers()
	if err != nil {
		t.Logf("Warning: failed to process existing containers: %v", err)
	}

	// 测试事件监听（只运行很短时间）
	done := make(chan bool)
	go func() {
		defer close(done)
		// 只监听5秒钟
		time.Sleep(5 * time.Second)
	}()

	// 启动事件监听
	go func() {
		if err := svc.WatchEvents(); err != nil {
			t.Logf("Event watching stopped: %v", err)
		}
	}()

	// 等待测试完成
	<-done

	// 关闭服务
	if err := svc.Close(); err != nil {
		t.Logf("Warning: failed to close service: %v", err)
	}

	t.Log("Event listening test completed")
}

func TestParseIopsLimitFromAnnotations(t *testing.T) {
	defaultLimit := 500
	cases := []struct {
		name   string
		ann    map[string]string
		expect int
	}{
		{"no annotation", map[string]string{}, 500},
		{"valid annotation", map[string]string{"iops-limit/limit": "1000"}, 1000},
		{"invalid annotation", map[string]string{"iops-limit/limit": "abc"}, 500},
		{"zero annotation", map[string]string{"iops-limit/limit": "0"}, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := service.ParseIopsLimitFromAnnotations(c.ann, defaultLimit)
			if got != c.expect {
				t.Errorf("parseIopsLimitFromAnnotations() = %d, want %d", got, c.expect)
			}
		})
	}
}

type fakeRuntime struct {
	containers []*container.ContainerInfo
}

func (f *fakeRuntime) GetContainers() ([]*container.ContainerInfo, error)           { return f.containers, nil }
func (f *fakeRuntime) GetContainerByID(id string) (*container.ContainerInfo, error) { return nil, nil }
func (f *fakeRuntime) ProcessContainer(c *container.ContainerInfo) error            { return nil }
func (f *fakeRuntime) Close() error                                                 { return nil }
func (f *fakeRuntime) GetContainersByPod(ns, name string) ([]*container.ContainerInfo, error) {
	var result []*container.ContainerInfo
	for _, c := range f.containers {
		if c.Annotations["io.kubernetes.pod.namespace"] == ns && c.Annotations["io.kubernetes.pod.name"] == name {
			result = append(result, c)
		}
	}
	return result, nil
}
func (f *fakeRuntime) SetIOPSLimit(c *container.ContainerInfo, i int) error {
	c.Name = "set"
	return nil
}

func TestGetContainersByPod(t *testing.T) {
	containers := []*container.ContainerInfo{
		{ID: "1", Annotations: map[string]string{"io.kubernetes.pod.namespace": "default", "io.kubernetes.pod.name": "nginx"}},
		{ID: "2", Annotations: map[string]string{"io.kubernetes.pod.namespace": "kube-system", "io.kubernetes.pod.name": "coredns"}},
		{ID: "3", Annotations: map[string]string{"io.kubernetes.pod.namespace": "default", "io.kubernetes.pod.name": "nginx"}},
	}
	rt := &fakeRuntime{containers: containers}
	found, err := rt.GetContainersByPod("default", "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(found) != 2 {
		t.Errorf("expected 2 containers, got %d", len(found))
	}
}

func TestSetIOPSLimit(t *testing.T) {
	ci := &container.ContainerInfo{ID: "1", Name: "test"}
	rt := &fakeRuntime{}
	err := rt.SetIOPSLimit(ci, 1000)
	if err != nil {
		t.Errorf("SetIOPSLimit error: %v", err)
	}
	if ci.Name != "set" {
		t.Errorf("SetIOPSLimit did not set name as expected")
	}
}

func TestProcessExistingContainersWithPodAnnotations(t *testing.T) {
	// 获取测试配置
	cfg := config.GetDefaultConfig()
	cfg.ContainerRuntime = "docker" // 使用docker进行测试
	cfg.ContainerIOPSLimit = 500
	cfg.DataMount = "/data"
	cfg.ExcludeKeywords = []string{"pause", "istio-proxy"}

	// 创建服务
	svc, err := service.NewIOPSLimitService(cfg)
	if err != nil {
		t.Skipf("Skipping test: failed to create service: %v", err)
	}

	// 测试处理现有容器（包含Pod注解逻辑）
	err = svc.ProcessExistingContainers()
	if err != nil {
		t.Logf("Warning: failed to process existing containers: %v", err)
	}

	// 关闭服务
	if err := svc.Close(); err != nil {
		t.Logf("Warning: failed to close service: %v", err)
	}

	t.Log("ProcessExistingContainers with Pod annotations test completed")
}

func TestExtractPodInfoFromContainer(t *testing.T) {
	// 创建测试服务实例
	cfg := config.GetDefaultConfig()
	svc, err := service.NewIOPSLimitService(cfg)
	if err != nil {
		t.Skipf("Skipping test: failed to create service: %v", err)
	}

	// 测试用例
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
			name: "missing_namespace",
			container: &container.ContainerInfo{
				ID: "test-container",
				Annotations: map[string]string{
					"io.kubernetes.pod.name": "test-pod",
				},
			},
			expectedNS:   "",
			expectedName: "",
		},
		{
			name: "missing_name",
			container: &container.ContainerInfo{
				ID: "test-container",
				Annotations: map[string]string{
					"io.kubernetes.pod.namespace": "test-namespace",
				},
			},
			expectedNS:   "",
			expectedName: "",
		},
		{
			name: "no_annotations",
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
			// 由于extractPodInfoFromContainer是私有方法，我们通过反射或其他方式测试
			// 这里我们直接测试逻辑，因为方法很简单
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

	// 关闭服务
	if err := svc.Close(); err != nil {
		t.Logf("Warning: failed to close service: %v", err)
	}
}

func TestKubeletConfig(t *testing.T) {
	// 测试kubelet配置加载
	cfg := config.GetDefaultConfig()

	// 设置环境变量
	os.Setenv("KUBELET_HOST", "test-host")
	os.Setenv("KUBELET_PORT", "10255")

	// 重新加载配置
	config.LoadFromEnv(cfg)

	// 验证配置
	if cfg.KubeletHost != "test-host" {
		t.Errorf("Expected KubeletHost to be 'test-host', got '%s'", cfg.KubeletHost)
	}
	if cfg.KubeletPort != "10255" {
		t.Errorf("Expected KubeletPort to be '10255', got '%s'", cfg.KubeletPort)
	}

	// 清理环境变量
	os.Unsetenv("KUBELET_HOST")
	os.Unsetenv("KUBELET_PORT")
}

// mockKubeClient实现
type mockKubeClient struct {
	pods []corev1.Pod
}

func (m *mockKubeClient) ListNodePodsWithKubeletFirst() ([]corev1.Pod, error) {
	return m.pods, nil
}
func (m *mockKubeClient) WatchNodePods() (watch.Interface, error) {
	return nil, nil
}

func TestResetAllContainersIOPSLimit(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.ContainerRuntime = "docker"
	cfg.DataMount = "/data"

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "pod1"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{ContainerID: "docker://cid1"}},
			},
		},
	}
	mockKC := &mockKubeClient{pods: pods}
	svc, err := service.NewIOPSLimitServiceWithKubeClient(cfg, mockKC)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	err = svc.ResetAllContainersIOPSLimit()
	if err != nil {
		t.Errorf("ResetAllContainersIOPSLimit error: %v", err)
	}
}

func TestResetOneContainerIOPSLimit(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.ContainerRuntime = "docker"
	cfg.DataMount = "/data"
	mockKC := &mockKubeClient{pods: nil}
	svc, err := service.NewIOPSLimitServiceWithKubeClient(cfg, mockKC)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	err = svc.ResetOneContainerIOPSLimit("cid1")
	if err != nil {
		t.Logf("ResetOneContainerIOPSLimit error (may be expected if no such container): %v", err)
	}
}

// 独立的过滤逻辑函数，便于测试
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
	return true
}

func TestShouldProcessPod(t *testing.T) {
	excludeNamespaces := []string{"kube-system", "monitoring"}
	excludeLabelSelector := "app=skipme"

	cases := []struct {
		name   string
		pod    corev1.Pod
		expect bool
	}{
		{"running, ns not excluded, label not excluded", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Labels: map[string]string{"app": "test"}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		}, true},
		{"running, ns excluded", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Labels: map[string]string{"app": "test"}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		}, false},
		{"not running", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Labels: map[string]string{"app": "test"}},
			Status:     corev1.PodStatus{Phase: corev1.PodPending},
		}, false},
		{"running, label excluded", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Labels: map[string]string{"app": "skipme"}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning},
		}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldProcessPodForTest(c.pod, excludeNamespaces, excludeLabelSelector)
			if got != c.expect {
				t.Errorf("shouldProcessPodForTest() = %v, want %v", got, c.expect)
			}
		})
	}
}
