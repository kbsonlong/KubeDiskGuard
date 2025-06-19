package service

import (
	"fmt"
	"log"
	"os"
	"reflect"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/detector"
	"iops-limit-service/pkg/kubeclient"
	"iops-limit-service/pkg/runtime"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
)

// IOPSLimitService IOPS限制服务
type IOPSLimitService struct {
	config     *config.Config
	runtime    container.Runtime
	kubeClient kubeclient.IKubeClient
}

// NewIOPSLimitService 创建IOPS限制服务
func NewIOPSLimitService(config *config.Config) (*IOPSLimitService, error) {
	service := &IOPSLimitService{
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
func (s *IOPSLimitService) ShouldSkipContainer(image, name string) bool {
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

// processPodContainers 处理单个Pod下所有容器的IOPS限速
func (s *IOPSLimitService) processPodContainers(pod corev1.Pod, iopsLimit int) {
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
		if s.ShouldSkipContainer(containerInfo.Image, containerInfo.Name) {
			log.Printf("Skip IOPS limit for container %s (excluded by keyword)", containerInfo.ID)
			continue
		}
		if err := s.runtime.SetIOPSLimit(containerInfo, iopsLimit); err != nil {
			log.Printf("Failed to set IOPS limit for container %s: %v", containerInfo.ID, err)
		} else {
			log.Printf("Applied IOPS limit for container %s (pod: %s/%s): %d", containerInfo.ID, pod.Namespace, pod.Name, iopsLimit)
		}
	}
}

// ShouldProcessPod 判断Pod是否需要处理（命名空间、labelSelector过滤）
func (s *IOPSLimitService) ShouldProcessPod(pod corev1.Pod) bool {
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
	return true
}

// ProcessExistingContainers 处理现有容器（以Pod为主索引）
func (s *IOPSLimitService) ProcessExistingContainers() error {
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
		iopsLimit := ParseIopsLimitFromAnnotations(pod.Annotations, s.config.ContainerIOPSLimit)
		s.processPodContainers(pod, iopsLimit)
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
func (s *IOPSLimitService) WatchEvents() error {
	return s.WatchPodEvents()
}

// WatchPodEvents 监听本节点Pod变化，动态调整IOPS
func (s *IOPSLimitService) WatchPodEvents() error {
	watcher, err := s.kubeClient.WatchNodePods()
	if err != nil {
		return err
	}
	// 修正：通过环境变量获取节点名
	nodeName := os.Getenv("NODE_NAME")
	log.Printf("Start watching pods on node: %s", nodeName)
	podAnnotations := make(map[string]struct {
		Annotations map[string]string
		IopsLimit   int
	})
	for event := range watcher.ResultChan() {
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		key := pod.Namespace + "/" + pod.Name
		switch event.Type {
		case watch.Modified:
			if !s.ShouldProcessPod(*pod) {
				continue
			}
			old := podAnnotations[key]
			newAnn := pod.Annotations
			iopsLimit := ParseIopsLimitFromAnnotations(newAnn, s.config.ContainerIOPSLimit)
			if reflect.DeepEqual(old.Annotations, newAnn) && old.IopsLimit == iopsLimit {
				continue
			}
			s.processPodContainers(*pod, iopsLimit)
			podAnnotations[key] = struct {
				Annotations map[string]string
				IopsLimit   int
			}{
				Annotations: copyMap(newAnn),
				IopsLimit:   iopsLimit,
			}
		case watch.Deleted:
			delete(podAnnotations, key)
		}
	}
	return nil
}

// ParseIopsLimitFromAnnotations 解析注解中的iops限制（导出）
func ParseIopsLimitFromAnnotations(ann map[string]string, defaultLimit int) int {
	if v, ok := ann["iops-limit/limit"]; ok {
		var val int
		_, err := fmt.Sscanf(v, "%d", &val)
		if err == nil && val > 0 {
			return val
		}
	}
	return defaultLimit
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
func (s *IOPSLimitService) Close() error {
	if s.runtime != nil {
		return s.runtime.Close()
	}
	return nil
}

// Run 运行服务
func (s *IOPSLimitService) Run() error {
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
func (s *IOPSLimitService) getNodePods() ([]corev1.Pod, error) {
	return s.kubeClient.ListNodePodsWithKubeletFirst()
}

// ResetAllContainersIOPSLimit 解除所有容器的IOPS限速
func (s *IOPSLimitService) ResetAllContainersIOPSLimit() error {
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
			if err := s.runtime.ResetIOPSLimit(containerInfo); err != nil {
				log.Printf("Failed to reset IOPS limit for container %s: %v", containerID, err)
			}
		}
	}
	return nil
}

// ResetOneContainerIOPSLimit 解除指定容器的IOPS限速
func (s *IOPSLimitService) ResetOneContainerIOPSLimit(containerID string) error {
	containerInfo, err := s.runtime.GetContainerByID(containerID)
	if err != nil {
		return err
	}
	return s.runtime.ResetIOPSLimit(containerInfo)
}

// 新增：支持注入mock kubeclient
func NewIOPSLimitServiceWithKubeClient(config *config.Config, kc kubeclient.IKubeClient) (*IOPSLimitService, error) {
	service := &IOPSLimitService{
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
