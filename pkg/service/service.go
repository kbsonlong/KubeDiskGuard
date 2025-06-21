package service

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"

	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/detector"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/kubelet"
	"KubeDiskGuard/pkg/runtime"
	"KubeDiskGuard/pkg/smartlimit"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
)

// KubeDiskGuardService 节点级磁盘IO资源守护与限速服务
type KubeDiskGuardService struct {
	config     *config.Config
	runtime    container.Runtime
	kubeClient kubeclient.IKubeClient
	smartLimit *smartlimit.SmartLimitManager
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

	// 初始化智能限速管理器
	if config.SmartLimitEnabled {
		cgroupMgr := cgroup.NewManager(config.CgroupVersion)

		// 初始化kubelet客户端
		var kubeletClient *kubelet.KubeletClient
		if config.SmartLimitUseKubeletAPI && config.KubeletHost != "" && config.KubeletPort != "" {
			var err error
			kubeletClient, err = kubelet.NewKubeletClient(
				config.KubeletHost,
				config.KubeletPort,
				config.KubeletTokenPath,
				config.KubeletCAPath,
				config.KubeletSkipVerify,
			)
			if err != nil {
				log.Printf("Failed to create kubelet client: %v", err)
			} else {
				log.Printf("Kubelet client initialized for host: %s:%s", config.KubeletHost, config.KubeletPort)
			}
		}

		// 类型断言
		kubeClient, ok := service.kubeClient.(*kubeclient.KubeClient)
		if !ok {
			log.Printf("Warning: kubeClient is not *kubeclient.KubeClient, smart limit may not work properly")
			kubeClient = nil
		}

		service.smartLimit = smartlimit.NewSmartLimitManager(config, kubeClient, kubeletClient, cgroupMgr)
		log.Printf("Smart limit manager initialized")
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
		containerInfo, err := s.runtime.GetContainerByID(containerID)
		if err != nil {
			log.Printf("Failed to get container info for %s: %v", containerID, err)
			continue
		}
		if s.ShouldSkipContainer(containerInfo.Image, containerInfo.Name) {
			log.Printf("Skip IOPS/BPS limit for container %s (excluded by keyword)", containerInfo.ID)
			continue
		}
		// 判断是否需要解除限速
		if readIopsVal == 0 && writeIopsVal == 0 && readBps == 0 && writeBps == 0 {
			if err := s.runtime.ResetLimits(containerInfo); err != nil {
				log.Printf("Failed to reset all limits for container %s: %v", containerInfo.ID, err)
			} else {
				log.Printf("Reset all limits for container %s (pod: %s/%s)", containerInfo.ID, pod.Namespace, pod.Name)
			}
			continue
		}
		// 一次性下发所有限速项
		if err := s.runtime.SetLimits(containerInfo, readIopsVal, writeIopsVal, readBps, writeBps); err != nil {
			log.Printf("Failed to set limits for container %s: %v", containerInfo.ID, err)
		} else {
			log.Printf("Applied limits for container %s (pod: %s/%s): riops=%d wiops=%d rbps=%d wbps=%d", containerInfo.ID, pod.Namespace, pod.Name, readIopsVal, writeIopsVal, readBps, writeBps)
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
		return err
	}
	// 修正：通过环境变量获取节点名
	nodeName := os.Getenv("NODE_NAME")
	log.Printf("Start watching pods on node: %s", nodeName)
	podAnnotations := make(map[string]struct {
		Annotations map[string]string
		ReadIops    int
		WriteIops   int
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
			readIops, writeIops := ParseIopsLimitFromAnnotations(newAnn, s.config.ContainerReadIOPSLimit, s.config.ContainerWriteIOPSLimit)
			if reflect.DeepEqual(old.Annotations, newAnn) && old.ReadIops == readIops && old.WriteIops == writeIops {
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
			delete(podAnnotations, key)
		}
	}
	return nil
}

// ParseIopsLimitFromAnnotations 从Pod注解中解析IOPS限制
func ParseIopsLimitFromAnnotations(ann map[string]string, defaultRead, defaultWrite int) (readIops, writeIops int) {
	if ann == nil {
		return defaultRead, defaultWrite
	}

	// 检查是否有智能限速注解
	if smartLimit, exists := ann["io-limit/smart-limit"]; exists && smartLimit == "true" {
		if autoIOPS, exists := ann["io-limit/auto-iops"]; exists {
			if iops, err := strconv.Atoi(autoIOPS); err == nil && iops > 0 {
				return iops, iops // 智能限速时读写使用相同值
			}
		}
	}

	// 优先级1: io-limit/iops (统一IOPS限制，最高优先级)
	if val, exists := ann["io-limit/iops"]; exists {
		if iops, err := strconv.Atoi(val); err == nil {
			return iops, iops
		}
	}

	// 优先级2: 分别设置读写IOPS
	readSet := false
	writeSet := false

	if val, exists := ann["io-limit/read-iops"]; exists {
		if iops, err := strconv.Atoi(val); err == nil {
			readIops = iops
			readSet = true
		}
	}

	if val, exists := ann["io-limit/write-iops"]; exists {
		if iops, err := strconv.Atoi(val); err == nil {
			writeIops = iops
			writeSet = true
		}
	}

	// 优先级3: 兼容旧格式
	if val, exists := ann["io-limit"]; exists {
		if iops, err := strconv.Atoi(val); err == nil && iops > 0 {
			return iops, iops
		}
	}

	if val, exists := ann["io-limit/read"]; exists {
		if iops, err := strconv.Atoi(val); err == nil && iops > 0 {
			readIops = iops
			readSet = true
		}
	}

	if val, exists := ann["io-limit/write"]; exists {
		if iops, err := strconv.Atoi(val); err == nil && iops > 0 {
			writeIops = iops
			writeSet = true
		}
	}

	// 如果某个值没有被设置，使用默认值
	if !readSet {
		readIops = defaultRead
	}
	if !writeSet {
		writeIops = defaultWrite
	}

	return readIops, writeIops
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
	// 停止智能限速监控
	if s.smartLimit != nil {
		s.smartLimit.Stop()
	}

	// 关闭运行时连接
	if s.runtime != nil {
		return s.runtime.Close()
	}
	return nil
}

// Run 运行服务
func (s *KubeDiskGuardService) Run() error {
	// 启动智能限速监控
	if s.smartLimit != nil {
		s.smartLimit.Start()
	}

	// 处理现有容器
	if err := s.ProcessExistingContainers(); err != nil {
		log.Printf("Failed to process existing containers: %v", err)
	}

	// 监听Pod事件
	return s.WatchPodEvents()
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

// ParseBpsLimitFromAnnotations 从Pod注解中解析BPS限制
func ParseBpsLimitFromAnnotations(ann map[string]string, defaultRead, defaultWrite int) (readBps, writeBps int) {
	if ann == nil {
		return defaultRead, defaultWrite
	}

	// 检查是否有智能限速注解
	if smartLimit, exists := ann["io-limit/smart-limit"]; exists && smartLimit == "true" {
		if autoBPS, exists := ann["io-limit/auto-bps"]; exists {
			if bps, err := strconv.Atoi(autoBPS); err == nil && bps > 0 {
				return bps, bps // 智能限速时读写使用相同值
			}
		}
	}

	// 优先级1: io-limit/bps (统一BPS限制，最高优先级)
	if val, exists := ann["io-limit/bps"]; exists {
		if bps, err := parseBpsValue(val); err == nil {
			return bps, bps
		}
	}

	// 优先级2: 分别设置读写BPS
	readSet := false
	writeSet := false

	if val, exists := ann["io-limit/read-bps"]; exists {
		if bps, err := parseBpsValue(val); err == nil {
			readBps = bps
			readSet = true
		}
	}

	if val, exists := ann["io-limit/write-bps"]; exists {
		if bps, err := parseBpsValue(val); err == nil {
			writeBps = bps
			writeSet = true
		}
	}

	// 如果某个值没有被设置，使用默认值
	if !readSet {
		readBps = defaultRead
	}
	if !writeSet {
		writeBps = defaultWrite
	}

	return readBps, writeBps
}

// parseBpsValue 支持纯数字（字节/秒），后续可扩展单位
func parseBpsValue(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}
