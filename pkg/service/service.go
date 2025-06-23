package service

import (
	"fmt"
	"log"
	"os"
	"reflect"

	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/detector"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/runtime"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
)

var (
	containerTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubediskguard_container_total",
		Help: "处理的容器总数",
	})
	containerSuccess = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubediskguard_container_success_total",
		Help: "成功设置限速的容器数",
	})
	containerFail = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubediskguard_container_fail_total",
		Help: "设置限速失败的容器数",
	})
	containerSkip = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubediskguard_container_skip_total",
		Help: "被跳过的容器数",
	})
	containerReset = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "kubediskguard_container_reset_total",
		Help: "被取消限速的容器数",
	})
)

func init() {
	prometheus.MustRegister(containerTotal, containerSuccess, containerFail, containerSkip, containerReset)
}

// KubeDiskGuardService 节点级磁盘IO资源守护与限速服务
type KubeDiskGuardService struct {
	config     *config.Config
	runtime    container.Runtime
	kubeClient kubeclient.IKubeClient
}

// NewKubeDiskGuardService 创建KubeDiskGuardService
func NewKubeDiskGuardService(config *config.Config) (*KubeDiskGuardService, error) {
	service := &KubeDiskGuardService{
		config: config,
	}

	// 自动检测运行时
	if config.ContainerRuntime == "auto" {
		config.ContainerRuntime = detector.DetectRuntime()
	}

	// 自动检测cgroup版本
	if config.CgroupVersion == "auto" {
		config.CgroupVersion = detector.DetectCgroupVersion()
	}

	log.Printf("Using container runtime: %s", config.ContainerRuntime)
	log.Printf("Detected cgroup version: %s", config.CgroupVersion)

	// 初始化运行时
	var err error
	switch config.ContainerRuntime {
	case "docker":
		service.runtime, err = runtime.NewDockerRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker runtime: %v", err)
		}
	case "containerd":
		service.runtime, err = runtime.NewContainerdRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create containerd runtime: %v", err)
		}
	default:
		return nil, fmt.Errorf("unsupported container runtime: %s", config.ContainerRuntime)
	}

	// 初始化kubeClient
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return nil, fmt.Errorf("NODE_NAME env is required")
	}
	service.kubeClient, err = kubeclient.NewKubeClient(nodeName, config.KubeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeclient: %v", err)
	}

	return service, nil
}

// ShouldSkipContainer 只做关键字过滤
func (s *KubeDiskGuardService) ShouldSkipContainer(image, name string) bool {
	for _, keyword := range s.config.ExcludeKeywords {
		if contains(image, keyword) || contains(name, keyword) {
			return true
		}
	}
	return false
}

// contains 检查字符串是否包含子字符串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))))
}

// containsSubstring 检查字符串中间是否包含子字符串
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// processPodContainers 处理单个Pod下所有容器的IOPS/BPS限速（支持读写分开）
func (s *KubeDiskGuardService) processPodContainers(pod corev1.Pod) {
	readIopsVal, writeIopsVal := ParseIopsLimitFromAnnotations(pod.Annotations, s.config.ContainerReadIOPSLimit, s.config.ContainerWriteIOPSLimit)
	readBps, writeBps := ParseBpsLimitFromAnnotations(pod.Annotations, s.config.ContainerReadBPSLimit, s.config.ContainerWriteBPSLimit)
	for _, cs := range pod.Status.ContainerStatuses {
		containerID := parseRuntimeID(cs.ContainerID)
		if containerID == "" {
			continue
		}
		containerTotal.Inc()
		containerInfo, err := s.runtime.GetContainerByID(containerID)
		if err != nil {
			log.Printf("Failed to get container info for %s: %v", containerID, err)
			containerFail.Inc()
			continue
		}
		if s.ShouldSkipContainer(containerInfo.Image, containerInfo.Name) {
			log.Printf("Skip IOPS/BPS limit for container %s (excluded by keyword)", containerInfo.ID)
			containerSkip.Inc()
			continue
		}
		// 判断是否需要解除限速
		if readIopsVal == 0 && writeIopsVal == 0 && readBps == 0 && writeBps == 0 {
			if err := s.runtime.ResetLimits(containerInfo); err != nil {
				log.Printf("Failed to reset all limits for container %s: %v", containerInfo.ID, err)
				containerFail.Inc()
			} else {
				log.Printf("Reset all limits for container %s (pod: %s/%s)", containerInfo.ID, pod.Namespace, pod.Name)
				containerReset.Inc()
			}
			continue
		}
		// 一次性下发所有限速项
		if err := s.runtime.SetLimits(containerInfo, readIopsVal, writeIopsVal, readBps, writeBps); err != nil {
			log.Printf("Failed to set limits for container %s: %v", containerInfo.ID, err)
			containerFail.Inc()
		} else {
			log.Printf("Applied limits for container %s (pod: %s/%s): riops=%d wiops=%d rbps=%d wbps=%d", containerInfo.ID, pod.Namespace, pod.Name, readIopsVal, writeIopsVal, readBps, writeBps)
			containerSuccess.Inc()
		}
	}
}

// ShouldProcessPod 判断Pod是否需要处理（命名空间、labelSelector过滤）
func (s *KubeDiskGuardService) ShouldProcessPod(pod corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, ns := range s.config.ExcludeNamespaces {
		if pod.Namespace == ns {
			return false
		}
	}
	if s.config.ExcludeLabelSelector != "" {
		selector, err := labels.Parse(s.config.ExcludeLabelSelector)
		if err == nil && selector.Matches(labels.Set(pod.Labels)) {
			return false
		}
	}
	// 新逻辑：所有业务容器的Started字段必须为true（为nil视为false）
	// 启动探针通过之后，Started字段才会被设置为true
	for _, cs := range pod.Status.ContainerStatuses {
		// if cs.State.Running == nil {
		if cs.Started == nil || !*cs.Started {
			return false
		}
	}
	return true
}

// ProcessExistingContainers 处理现有容器（以Pod为主索引）
func (s *KubeDiskGuardService) ProcessExistingContainers() error {
	pods, err := s.getNodePods()
	if err != nil {
		log.Printf("Failed to get node pods: %v", err)
		return err
	}

	for _, pod := range pods {
		fmt.Printf(pod.Name)
		if !s.ShouldProcessPod(pod) {
			continue
		}
		// 解析注解
		s.processPodContainers(pod)
	}
	return nil
}

// parseRuntimeID 解析K8s ContainerID字段，去掉前缀（如docker://、containerd://）
func parseRuntimeID(k8sID string) string {
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

// WatchEvents 监听事件
func (s *KubeDiskGuardService) WatchEvents() error {
	return s.WatchPodEvents()
}

// WatchPodEvents 监听本节点Pod变化，动态调整IOPS
func (s *KubeDiskGuardService) WatchPodEvents() error {
	watcher, err := s.kubeClient.WatchNodePods()
	if err != nil {
		log.Printf("[DEBUG] WatchNodePods failed: %v", err)
		return err
	}
	// 修正：通过环境变量获取节点名
	nodeName := os.Getenv("NODE_NAME")
	log.Printf("Start watching pods on node: %s", nodeName)
	log.Printf("[DEBUG] Watcher created, entering event loop...")
	podAnnotations := make(map[string]struct {
		Annotations map[string]string
		ReadIops    int
		WriteIops   int
	})
	ch := watcher.ResultChan()
	if ch == nil {
		log.Printf("[DEBUG] watcher.ResultChan() is nil! Watcher: %#v", watcher)
		return fmt.Errorf("watcher.ResultChan() is nil")
	}
	for event := range ch {
		log.Printf("[DEBUG] Received event: %v", event.Type)
		if event.Object == nil {
			log.Printf("[DEBUG] Event object is nil")
			continue
		}
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			log.Printf("[DEBUG] Event object is not *corev1.Pod, got: %T", event.Object)
			continue
		}
		key := pod.Namespace + "/" + pod.Name
		switch event.Type {
		case watch.Modified:
			log.Printf("[DEBUG] Modified event for pod: %s", key)
			if !s.ShouldProcessPod(*pod) {
				log.Printf("[DEBUG] Pod %s should not be processed", key)
				continue
			}
			old := podAnnotations[key]
			newAnn := pod.Annotations
			readIops, writeIops := ParseIopsLimitFromAnnotations(newAnn, s.config.ContainerReadIOPSLimit, s.config.ContainerWriteIOPSLimit)
			if reflect.DeepEqual(old.Annotations, newAnn) && old.ReadIops == readIops && old.WriteIops == writeIops {
				log.Printf("[DEBUG] Pod %s annotations/iops unchanged, skipping", key)
				continue
			}
			s.processPodContainers(*pod)
			podAnnotations[key] = struct {
				Annotations map[string]string
				ReadIops    int
				WriteIops   int
			}{
				Annotations: copyMap(newAnn),
				ReadIops:    readIops,
				WriteIops:   writeIops,
			}
		case watch.Deleted:
			log.Printf("[DEBUG] Deleted event for pod: %s", key)
			delete(podAnnotations, key)
		}
	}
	log.Printf("[DEBUG] Event loop exited (watcher channel closed)")
	return nil
}

// ParseIopsLimitFromAnnotations 解析注解中的iops限制（分别支持读写）
func ParseIopsLimitFromAnnotations(ann map[string]string, defaultRead, defaultWrite int) (readIops, writeIops int) {
	readIops, writeIops = defaultRead, defaultWrite
	if v, ok := ann["iops-limit/read-iops"]; ok {
		var val int
		_, err := fmt.Sscanf(v, "%d", &val)
		if err == nil && val >= 0 {
			readIops = val
		}
	}
	if v, ok := ann["iops-limit/write-iops"]; ok {
		var val int
		_, err := fmt.Sscanf(v, "%d", &val)
		if err == nil && val >= 0 {
			writeIops = val
		}
	}
	// 通用iops注解，若存在，覆盖读写
	if v, ok := ann["iops-limit/iops"]; ok {
		var val int
		_, err := fmt.Sscanf(v, "%d", &val)
		if err == nil && val >= 0 {
			readIops, writeIops = val, val
		}
	}
	return
}

// copyMap 深拷贝map，防止引用问题
func copyMap(src map[string]string) map[string]string {
	dst := make(map[string]string)
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Close 关闭服务
func (s *KubeDiskGuardService) Close() error {
	if s.runtime != nil {
		return s.runtime.Close()
	}
	return nil
}

// Run 运行服务
func (s *KubeDiskGuardService) Run() error {
	log.Println("Starting IOPS limit service...")

	// 确保在服务结束时关闭运行时连接
	defer func() {
		if err := s.Close(); err != nil {
			log.Printf("Error closing runtime connection: %v", err)
		}
	}()

	// 处理现有容器
	if err := s.ProcessExistingContainers(); err != nil {
		log.Printf("Failed to process existing containers: %v", err)
	}

	// 监听新容器事件
	return s.WatchEvents()
}

// getNodePods 获取本节点的所有Pod（优先使用kubelet API，fallback到API Server）
func (s *KubeDiskGuardService) getNodePods() ([]corev1.Pod, error) {
	return s.kubeClient.ListNodePodsWithKubeletFirst()
}

// ResetAllContainersIOPSLimit 解除所有容器的IOPS限速
func (s *KubeDiskGuardService) ResetAllContainersIOPSLimit() error {
	pods, err := s.getNodePods()
	if err != nil {
		return err
	}
	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			containerID := parseRuntimeID(cs.ContainerID)
			if containerID == "" {
				continue
			}
			containerInfo, err := s.runtime.GetContainerByID(containerID)
			if err != nil {
				log.Printf("Failed to get container info for %s: %v", containerID, err)
				continue
			}
			if err := s.runtime.ResetLimits(containerInfo); err != nil {
				log.Printf("Failed to reset IOPS limit for container %s: %v", containerID, err)
			}
		}
	}
	return nil
}

// 新增：支持注入mock kubeclient
func NewKubeDiskGuardServiceWithKubeClient(config *config.Config, kc kubeclient.IKubeClient) (*KubeDiskGuardService, error) {
	service := &KubeDiskGuardService{
		config:     config,
		kubeClient: kc,
	}
	// 自动检测运行时
	if config.ContainerRuntime == "auto" {
		config.ContainerRuntime = detector.DetectRuntime()
	}
	if config.CgroupVersion == "auto" {
		config.CgroupVersion = detector.DetectCgroupVersion()
	}
	var err error
	switch config.ContainerRuntime {
	case "docker":
		service.runtime, err = runtime.NewDockerRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker runtime: %v", err)
		}
	case "containerd":
		service.runtime, err = runtime.NewContainerdRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create containerd runtime: %v", err)
		}
	default:
		return nil, fmt.Errorf("unsupported container runtime: %s", config.ContainerRuntime)
	}
	return service, nil
}

// ParseBpsLimitFromAnnotations 解析注解中的带宽限制（字节/秒）
func ParseBpsLimitFromAnnotations(ann map[string]string, defaultRead, defaultWrite int) (readBps, writeBps int) {
	// 优先单独注解
	readBps, writeBps = defaultRead, defaultWrite
	if v, ok := ann["iops-limit/read-bps"]; ok {
		if val, err := parseBpsValue(v); err == nil && val >= 0 {
			readBps = val
		}
	}
	if v, ok := ann["iops-limit/write-bps"]; ok {
		if val, err := parseBpsValue(v); err == nil && val >= 0 {
			writeBps = val
		}
	}
	// 通用bps注解，若存在，覆盖读写
	if v, ok := ann["iops-limit/bps"]; ok {
		if val, err := parseBpsValue(v); err == nil && val >= 0 {
			readBps, writeBps = val, val
		}
	}
	return
}

// parseBpsValue 支持纯数字（字节/秒），后续可扩展单位
func parseBpsValue(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
