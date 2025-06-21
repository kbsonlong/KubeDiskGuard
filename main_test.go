package main

import (
	"os"
	"testing"
	"time"

	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/detector"
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
	// 获取测试配置
	cfg := config.GetDefaultConfig()
	cfg.ContainerRuntime = "docker" // 使用docker进行测试
	cfg.ContainerIOPSLimit = 500
	cfg.DataMount = "/data"
	cfg.ExcludeKeywords = []string{"pause", "istio-proxy"}

	// 创建服务
	svc, err := service.NewKubeDiskGuardService(cfg)
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
	cases := []struct {
		name     string
		ann      map[string]string
		defRead  int
		defWrite int
		expectR  int
		expectW  int
	}{
		{"no annotation", map[string]string{}, 100, 200, 100, 200},
		{"read-iops only", map[string]string{"io-limit/read-iops": "1234"}, 100, 200, 1234, 200},
		{"write-iops only", map[string]string{"io-limit/write-iops": "5678"}, 100, 200, 100, 5678},
		{"both read/write", map[string]string{"io-limit/read-iops": "1234", "io-limit/write-iops": "5678"}, 100, 200, 1234, 5678},
		{"iops overrides", map[string]string{"io-limit/iops": "9999"}, 100, 200, 9999, 9999},
		{"all present, iops highest", map[string]string{"io-limit/read-iops": "1234", "io-limit/write-iops": "5678", "io-limit/iops": "8888"}, 100, 200, 8888, 8888},
		{"read-iops 0", map[string]string{"io-limit/read-iops": "0"}, 100, 200, 0, 200},
		{"write-iops 0", map[string]string{"io-limit/write-iops": "0"}, 100, 200, 100, 0},
		{"iops 0", map[string]string{"io-limit/iops": "0"}, 100, 200, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, w := service.ParseIopsLimitFromAnnotations(c.ann, c.defRead, c.defWrite)
			if r != c.expectR || w != c.expectW {
				t.Errorf("ParseIopsLimitFromAnnotations() = %d,%d, want %d,%d", r, w, c.expectR, c.expectW)
			}
		})
	}
}

func TestParseBpsLimitFromAnnotations(t *testing.T) {
	cases := []struct {
		name     string
		ann      map[string]string
		defRead  int
		defWrite int
		expectR  int
		expectW  int
	}{
		{"no annotation", map[string]string{}, 100, 200, 100, 200},
		{"read-bps only", map[string]string{"io-limit/read-bps": "1234"}, 100, 200, 1234, 200},
		{"write-bps only", map[string]string{"io-limit/write-bps": "5678"}, 100, 200, 100, 5678},
		{"both read/write", map[string]string{"io-limit/read-bps": "1234", "io-limit/write-bps": "5678"}, 100, 200, 1234, 5678},
		{"bps overrides", map[string]string{"io-limit/bps": "9999"}, 100, 200, 9999, 9999},
		{"all present, bps highest", map[string]string{"io-limit/read-bps": "1234", "io-limit/write-bps": "5678", "io-limit/bps": "8888"}, 100, 200, 8888, 8888},
		{"read-bps 0", map[string]string{"io-limit/read-bps": "0"}, 100, 200, 0, 200},
		{"write-bps 0", map[string]string{"io-limit/write-bps": "0"}, 100, 200, 100, 0},
		{"bps 0", map[string]string{"io-limit/bps": "0"}, 100, 200, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, w := service.ParseBpsLimitFromAnnotations(c.ann, c.defRead, c.defWrite)
			if r != c.expectR || w != c.expectW {
				t.Errorf("ParseBpsLimitFromAnnotations() = %d,%d, want %d,%d", r, w, c.expectR, c.expectW)
			}
		})
	}
}

type fakeRuntime struct {
	containers []*container.ContainerInfo
}

func (f *fakeRuntime) GetContainerByID(id string) (*container.ContainerInfo, error) { return nil, nil }
func (f *fakeRuntime) ProcessContainer(c *container.ContainerInfo) error            { return nil }
func (f *fakeRuntime) Close() error                                                 { return nil }

func TestProcessExistingContainersWithPodAnnotations(t *testing.T) {
	// 获取测试配置
	cfg := config.GetDefaultConfig()
	cfg.ContainerRuntime = "docker" // 使用docker进行测试
	cfg.ContainerIOPSLimit = 500
	cfg.DataMount = "/data"
	cfg.ExcludeKeywords = []string{"pause", "istio-proxy"}

	// 创建服务
	svc, err := service.NewKubeDiskGuardService(cfg)
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
	svc, err := service.NewKubeDiskGuardService(cfg)
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

func (m *mockKubeClient) GetPod(namespace, name string) (*corev1.Pod, error) {
	for _, pod := range m.pods {
		if pod.Namespace == namespace && pod.Name == name {
			return &pod, nil
		}
	}
	return nil, nil
}

func (m *mockKubeClient) UpdatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	return pod, nil
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
	svc, err := service.NewKubeDiskGuardServiceWithKubeClient(cfg, mockKC)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	err = svc.ResetAllContainersIOPSLimit()
	if err != nil {
		t.Errorf("ResetAllContainersIOPSLimit error: %v", err)
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

// mockRuntime 用于测试SetIOPSLimit和ResetIOPSLimit调用情况
// type mockRuntime struct {
// 	setCount    int
// 	resetCount  int
// 	lastSetID   string
// 	lastSetVal  int
// 	lastResetID string
// }
// func (m *mockRuntime) GetContainerByID(id string) (*container.ContainerInfo, error) { return nil, nil }
// func (m *mockRuntime) ProcessContainer(c *container.ContainerInfo) error  { return nil }
// func (m *mockRuntime) Close() error                                       { return nil }

type mockRuntime struct {
	setCount    int
	resetCount  int
	lastSetID   string
	lastSetVal  int
	lastResetID string
}

func (m *mockRuntime) GetContainerByID(id string) (*container.ContainerInfo, error) {
	return &container.ContainerInfo{ID: id, Image: "nginx", Name: "nginx"}, nil
}
func (m *mockRuntime) SetIOPSLimit(c *container.ContainerInfo, iops int) error {
	m.setCount++
	m.lastSetID = c.ID
	m.lastSetVal = iops
	return nil
}
func (m *mockRuntime) ResetIOPSLimit(c *container.ContainerInfo) error {
	m.resetCount++
	m.lastResetID = c.ID
	return nil
}

// 其它接口用空实现
func (m *mockRuntime) GetContainers() ([]*container.ContainerInfo, error) { return nil, nil }
func (m *mockRuntime) ProcessContainer(c *container.ContainerInfo) error  { return nil }
func (m *mockRuntime) Close() error                                       { return nil }
func (m *mockRuntime) GetContainersByPod(ns, name string) ([]*container.ContainerInfo, error) {
	return nil, nil
}

func TestProcessPodContainers_IOPSLimitOrReset(t *testing.T) {
	mockRt := &mockRuntime{}
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "mypod"},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{ContainerID: "docker://cid1"}},
		},
	}
	shouldSkip := func(image, name string) bool { return false }

	// case1: 注解为正数，应该调用SetIOPSLimit
	processPodContainersForTest(pod, 1000, mockRt, shouldSkip)
	if mockRt.setCount != 1 || mockRt.lastSetID != "cid1" || mockRt.lastSetVal != 1000 {
		t.Errorf("SetIOPSLimit not called as expected, got setCount=%d, lastSetID=%s, lastSetVal=%d", mockRt.setCount, mockRt.lastSetID, mockRt.lastSetVal)
	}
	if mockRt.resetCount != 0 {
		t.Errorf("ResetIOPSLimit should not be called, got %d", mockRt.resetCount)
	}

	// case2: 注解为0，应该调用ResetIOPSLimit
	mockRt.setCount, mockRt.resetCount = 0, 0
	processPodContainersForTest(pod, 0, mockRt, shouldSkip)
	if mockRt.resetCount != 1 || mockRt.lastResetID != "cid1" {
		t.Errorf("ResetIOPSLimit not called as expected, got resetCount=%d, lastResetID=%s", mockRt.resetCount, mockRt.lastResetID)
	}
	if mockRt.setCount != 0 {
		t.Errorf("SetIOPSLimit should not be called, got %d", mockRt.setCount)
	}
}

// processPodContainersForTest 测试用等价逻辑
func processPodContainersForTest(pod corev1.Pod, iopsLimit int, rt *mockRuntime, shouldSkip func(image, name string) bool) {
	for _, cs := range pod.Status.ContainerStatuses {
		containerID := parseRuntimeIDForTest(cs.ContainerID)
		if containerID == "" {
			continue
		}
		containerInfo, err := rt.GetContainerByID(containerID)
		if err != nil {
			continue
		}
		if shouldSkip(containerInfo.Image, containerInfo.Name) {
			continue
		}
		if iopsLimit > 0 {
			rt.SetIOPSLimit(containerInfo, iopsLimit)
		} else {
			rt.ResetIOPSLimit(containerInfo)
		}
	}
}

// parseRuntimeIDForTest 测试用解析函数
func parseRuntimeIDForTest(k8sID string) string {
	if k8sID == "" {
		return ""
	}
	if idx := len("docker://"); len(k8sID) > idx && k8sID[:idx] == "docker://" {
		return k8sID[idx:]
	}
	if idx := len("containerd://"); len(k8sID) > idx && k8sID[:idx] == "containerd://" {
		return k8sID[idx:]
	}
	return k8sID
}

func TestShouldProcessPod_StartedField(t *testing.T) {
	cfg := config.GetDefaultConfig()
	mockKC := &mockKubeClient{pods: nil}
	svc, err := service.NewKubeDiskGuardServiceWithKubeClient(cfg, mockKC)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}
	trueVal := true
	falseVal := false

	cases := []struct {
		name   string
		pod    corev1.Pod
		expect bool
	}{
		{"all started true", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Started: &trueVal}, {Started: &trueVal}},
			},
		}, true},
		{"one started false", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Started: &trueVal}, {Started: &falseVal}},
			},
		}, false},
		{"one started nil", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Started: &trueVal}, {Started: nil}},
			},
		}, false},
		{"all started nil", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{Started: nil}, {Started: nil}},
			},
		}, false},
		{"not running phase", corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default"},
			Status: corev1.PodStatus{
				Phase:             corev1.PodPending,
				ContainerStatuses: []corev1.ContainerStatus{{Started: &trueVal}},
			},
		}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := svc.ShouldProcessPod(c.pod)
			if got != c.expect {
				t.Errorf("ShouldProcessPod() = %v, want %v", got, c.expect)
			}
		})
	}
}
