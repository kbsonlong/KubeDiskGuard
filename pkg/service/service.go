package service

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

	"KubeDiskGuard/pkg/annotationkeys"
	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/detector"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/runtime"
	"KubeDiskGuard/pkg/smartlimit"

	"github.com/docker/go-units"
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
	Config     *config.Config
	runtime    container.Runtime
	kubeClient kubeclient.IKubeClient
	smartLimit *smartlimit.SmartLimitManager
}

// NewKubeDiskGuardService 创建KubeDiskGuardService
func NewKubeDiskGuardService(cfg *config.Config) (*KubeDiskGuardService, error) {
	service := &KubeDiskGuardService{
		Config: cfg,
	}

	if cfg.ContainerRuntime == "auto" {
		cfg.ContainerRuntime = detector.DetectRuntime()
	}
	if cfg.CgroupVersion == "auto" {
		cfg.CgroupVersion = detector.DetectCgroupVersion()
	}

	log.Printf("Using container runtime: %s", cfg.ContainerRuntime)
	log.Printf("Detected cgroup version: %s", cfg.CgroupVersion)

	var err error
	switch cfg.ContainerRuntime {
	case "docker":
		service.runtime, err = runtime.NewDockerRuntime(cfg)
	case "containerd":
		service.runtime, err = runtime.NewContainerdRuntime(cfg)
	default:
		return nil, fmt.Errorf("unsupported container runtime: %s", cfg.ContainerRuntime)
	}
	if err != nil {
		return nil, err
	}

	// 只有在智能限速启用时才创建 kubeclient
	if cfg.SmartLimitEnabled {
		nodeName := os.Getenv("NODE_NAME")
		// 当不使用 kubelet API 时，NODE_NAME 是必需的
		if !cfg.SmartLimitUseKubeletAPI && nodeName == "" {
			return nil, fmt.Errorf("NODE_NAME env is required when smart limit is enabled and not using kubelet API")
		}
		// 当使用 kubelet API 时，如果没有 NODE_NAME，使用默认值
		if cfg.SmartLimitUseKubeletAPI && nodeName == "" {
			nodeName = "localhost"
			log.Printf("Using kubelet API mode, NODE_NAME not set, using default: %s", nodeName)
		}
		kubeClient, err := kubeclient.NewKubeClientWithConfig(nodeName, cfg.KubeConfigPath, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubeclient: %v", err)
		}
		service.kubeClient = kubeClient

		cgroupMgr := cgroup.NewManager(cfg.CgroupVersion)
		service.smartLimit = smartlimit.NewSmartLimitManager(cfg, service.kubeClient, cgroupMgr)
		log.Printf("Smart limit manager initialized")
	} else {
		log.Printf("Smart limit disabled, skipping kubeclient creation")
	}

	return service, nil
}

func (s *KubeDiskGuardService) ShouldSkipContainer(image, name string) bool {
	for _, keyword := range s.Config.ExcludeKeywords {
		if strings.Contains(image, keyword) || strings.Contains(name, keyword) {
			return true
		}
	}
	return false
}

func (s *KubeDiskGuardService) processPodContainers(pod corev1.Pod) {
	prefix := s.Config.SmartLimitAnnotationPrefix
	readIopsVal, writeIopsVal := ParseIopsLimitFromAnnotations(pod.Annotations, s.Config.ContainerReadIOPSLimit, s.Config.ContainerWriteIOPSLimit, prefix)
	readBps, writeBps := ParseBpsLimitFromAnnotations(pod.Annotations, s.Config.ContainerReadBPSLimit, s.Config.ContainerWriteBPSLimit, prefix)

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

		if readIopsVal == 0 && writeIopsVal == 0 && readBps == 0 && writeBps == 0 {
			if err := s.runtime.ResetLimits(containerInfo); err != nil {
				log.Printf("Failed to reset all limits for container %s: %v", containerInfo.ID, err)
				containerFail.Inc()
			} else {
				log.Printf("Successfully reset all limits for container %s", containerInfo.ID)
				log.Printf("Reset all limits for container %s (pod: %s/%s)", containerInfo.ID, pod.Namespace, pod.Name)
				containerReset.Inc()
			}
			continue
		}

		if err := s.runtime.SetLimits(containerInfo, readIopsVal, writeIopsVal, readBps, writeBps); err != nil {
			log.Printf("Failed to set limits for container %s: %v", containerInfo.ID, err)
			containerFail.Inc()
		} else {
			log.Printf("Successfully set limits for container %s: riops=%d, wiops=%d, rbps=%d, wbps=%d", containerInfo.ID, readIopsVal, writeIopsVal, readBps, writeBps)
			log.Printf("Applied limits for container %s (pod: %s/%s): riops=%d wiops=%d rbps=%d wbps=%d", containerInfo.ID, pod.Namespace, pod.Name, readIopsVal, writeIopsVal, readBps, writeBps)
			containerSuccess.Inc()
		}
	}
}

func (s *KubeDiskGuardService) ShouldProcessPod(pod corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, ns := range s.Config.ExcludeNamespaces {
		if pod.Namespace == ns {
			return false
		}
	}
	if s.Config.ExcludeLabelSelector != "" {
		selector, err := labels.Parse(s.Config.ExcludeLabelSelector)
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

// ProcessExistingContainers 处理现有容器（以Pod为主索引）
func (s *KubeDiskGuardService) ProcessExistingContainers() error {
	// 如果 kubeClient 为 nil（智能限速禁用），则跳过处理
	if s.kubeClient == nil {
		log.Println("KubeClient is nil, skipping existing containers processing (smart limit disabled)")
		return nil
	}

	pods, err := s.kubeClient.ListNodePodsWithKubeletFirst()
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
	// 如果 kubeClient 为 nil（智能限速禁用），则跳过监听
	if s.kubeClient == nil {
		log.Println("KubeClient is nil, skipping pod events watching (smart limit disabled)")
		return nil
	}

	watcher, err := s.kubeClient.WatchNodePods()
	if err != nil {
		log.Printf("[DEBUG] WatchNodePods failed: %v", err)
		return err
	}
	// 修正：通过环境变量获取节点名
	nodeName := os.Getenv("NODE_NAME")
	log.Printf("Start watching pods on node: %s", nodeName)
	log.Printf("[DEBUG] Watcher created, entering event loop...")
	podAnnotations := make(map[string]PodAnnotationState)
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

			old, exists := podAnnotations[key]
			newAnn := pod.Annotations
			readIops, writeIops := ParseIopsLimitFromAnnotations(newAnn, s.Config.ContainerReadIOPSLimit, s.Config.ContainerWriteIOPSLimit, s.Config.SmartLimitAnnotationPrefix)
			readBps, writeBps := ParseBpsLimitFromAnnotations(newAnn, s.Config.ContainerReadBPSLimit, s.Config.ContainerWriteBPSLimit, s.Config.SmartLimitAnnotationPrefix)

			if exists && reflect.DeepEqual(old.Annotations, newAnn) &&
				old.ReadIops == readIops && old.WriteIops == writeIops &&
				old.ReadBps == readBps && old.WriteBps == writeBps {
				continue
			}

			s.processPodContainers(*pod)
			podAnnotations[key] = PodAnnotationState{
				Annotations: newAnn,
				ReadIops:    readIops,
				WriteIops:   writeIops,
				ReadBps:     readBps,
				WriteBps:    writeBps,
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
func ParseIopsLimitFromAnnotations(annotations map[string]string, defaultReadIops, defaultWriteIops int, prefix string) (int, int) {
	readIops, writeIops := defaultReadIops, defaultWriteIops
	annotationPrefix := prefix + "/"

	if val, ok := annotations[annotationPrefix+annotationkeys.RemovedAnnotationKey]; ok && val == "true" {
		return 0, 0
	}

	useSmart := hasSmartLimitAnnotation(annotations, prefix)

	if useSmart {
		if iops, ok := annotations[annotationPrefix+annotationkeys.IopsAnnotationKey]; ok {
			if value, err := strconv.Atoi(iops); err == nil {
				return value, value
			}
		}
		if riops, ok := annotations[annotationPrefix+annotationkeys.ReadIopsAnnotationKey]; ok {
			if value, err := strconv.Atoi(riops); err == nil {
				readIops = value
			}
		}
		if wiops, ok := annotations[annotationPrefix+annotationkeys.WriteIopsAnnotationKey]; ok {
			if value, err := strconv.Atoi(wiops); err == nil {
				writeIops = value
			}
		}
		return readIops, writeIops
	}

	// Fallback to legacy only if no smart limit annotations are present
	if iops, ok := annotations[annotationkeys.LegacyIopsAnnotationKey]; ok {
		if value, err := strconv.Atoi(iops); err == nil {
			return value, value
		}
	}
	if riops, ok := annotations[annotationkeys.LegacyReadIopsAnnotationKey]; ok {
		if value, err := strconv.Atoi(riops); err == nil {
			readIops = value
		}
	}
	if wiops, ok := annotations[annotationkeys.LegacyWriteIopsAnnotationKey]; ok {
		if value, err := strconv.Atoi(wiops); err == nil {
			writeIops = value
		}
	}

	return readIops, writeIops
}

func (s *KubeDiskGuardService) Close() error {
	if s.smartLimit != nil {
		s.smartLimit.Stop()
	}

	return s.runtime.Close()
}

// GetSmartLimitManager 返回智能限速管理器
func (s *KubeDiskGuardService) GetSmartLimitManager() *smartlimit.SmartLimitManager {
	return s.smartLimit
}

func (s *KubeDiskGuardService) Run() error {
	if s.smartLimit != nil {
		s.smartLimit.Start()
	}

	// 如果 kubeClient 为 nil（智能限速禁用），则跳过 Pod 事件监听
	if s.kubeClient == nil {
		log.Println("KubeClient is nil, skipping pod event monitoring (smart limit disabled)")
		// 只启动智能限速管理器，不进行 Pod 监听
		select {} // 阻塞等待
	}

	pods, err := s.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		// 在 kubelet API 模式下，如果连接失败，记录警告但不退出服务
		if s.Config.SmartLimitUseKubeletAPI {
			log.Printf("Warning: failed to list existing pods in kubelet API mode: %v", err)
			log.Println("Continuing without initial pod list, will rely on smart limit manager...")
			// 只启动智能限速管理器，不进行 Pod 监听
			select {} // 阻塞等待
		}
		return fmt.Errorf("failed to list existing pods: %v", err)
	}

	podAnnotations := make(map[string]PodAnnotationState)
	for _, pod := range pods {
		if !s.ShouldProcessPod(pod) {
			continue
		}
		s.processPodContainers(pod)
		key := pod.Namespace + "/" + pod.Name
		readIops, writeIops := ParseIopsLimitFromAnnotations(pod.Annotations, s.Config.ContainerReadIOPSLimit, s.Config.ContainerWriteIOPSLimit, s.Config.SmartLimitAnnotationPrefix)
		readBps, writeBps := ParseBpsLimitFromAnnotations(pod.Annotations, s.Config.ContainerReadBPSLimit, s.Config.ContainerWriteBPSLimit, s.Config.SmartLimitAnnotationPrefix)
		podAnnotations[key] = PodAnnotationState{
			Annotations: pod.Annotations,
			ReadIops:    readIops,
			WriteIops:   writeIops,
			ReadBps:     readBps,
			WriteBps:    writeBps,
		}
	}

	watcher, err := s.kubeClient.WatchNodePods()
	if err != nil {
		// 在 kubelet API 模式下，如果监听失败，记录警告但不退出服务
		if s.Config.SmartLimitUseKubeletAPI {
			log.Printf("Warning: failed to watch pods in kubelet API mode: %v", err)
			log.Println("Continuing without pod watching, will rely on smart limit manager...")
			// 只启动智能限速管理器，不进行 Pod 监听
			select {} // 阻塞等待
		}
		return fmt.Errorf("failed to watch pods: %v", err)
	}
	defer watcher.Stop()

	log.Println("Start watching pod events...")
	s.watchPodEvents(watcher, podAnnotations)
	return nil
}

// watchPodEvents 监听Pod事件的内部方法
func (s *KubeDiskGuardService) watchPodEvents(watcher watch.Interface, podAnnotations map[string]PodAnnotationState) {
	ch := watcher.ResultChan()
	for event := range ch {
		if event.Object == nil {
			continue
		}
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

			old, exists := podAnnotations[key]
			newAnn := pod.Annotations
			readIops, writeIops := ParseIopsLimitFromAnnotations(newAnn, s.Config.ContainerReadIOPSLimit, s.Config.ContainerWriteIOPSLimit, s.Config.SmartLimitAnnotationPrefix)
			readBps, writeBps := ParseBpsLimitFromAnnotations(newAnn, s.Config.ContainerReadBPSLimit, s.Config.ContainerWriteBPSLimit, s.Config.SmartLimitAnnotationPrefix)

			if exists && reflect.DeepEqual(old.Annotations, newAnn) &&
				old.ReadIops == readIops && old.WriteIops == writeIops &&
				old.ReadBps == readBps && old.WriteBps == writeBps {
				continue
			}

			s.processPodContainers(*pod)
			podAnnotations[key] = PodAnnotationState{
				Annotations: newAnn,
				ReadIops:    readIops,
				WriteIops:   writeIops,
				ReadBps:     readBps,
				WriteBps:    writeBps,
			}
		case watch.Deleted:
			delete(podAnnotations, key)
		}
	}
}

func (s *KubeDiskGuardService) ResetAllContainersIOPSLimit() error {
	pods, err := s.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}
	for _, pod := range pods {
		if !s.ShouldProcessPod(pod) {
			continue
		}
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

// NewKubeDiskGuardServiceWithKubeClient is a constructor for testing with a mock kubeclient
func NewKubeDiskGuardServiceWithKubeClient(cfg *config.Config, kc kubeclient.IKubeClient) (*KubeDiskGuardService, error) {
	service := &KubeDiskGuardService{
		Config:     cfg,
		kubeClient: kc,
	}

	var err error
	service.runtime, err = runtime.NewDockerRuntime(cfg)
	if err != nil {
		return nil, err
	}

	if cfg.SmartLimitEnabled {
		cgroupMgr := cgroup.NewManager(cfg.CgroupVersion)
		service.smartLimit = smartlimit.NewSmartLimitManager(cfg, kc, cgroupMgr)
	}

	return service, nil
}

func hasSmartLimitAnnotation(annotations map[string]string, prefix string) bool {
	annotationPrefix := prefix + "/"
	smartKeys := []string{
		annotationkeys.IopsAnnotationKey, annotationkeys.ReadIopsAnnotationKey, annotationkeys.WriteIopsAnnotationKey,
		annotationkeys.BpsAnnotationKey, annotationkeys.ReadBpsAnnotationKey, annotationkeys.WriteBpsAnnotationKey,
	}
	for _, key := range smartKeys {
		if _, ok := annotations[annotationPrefix+key]; ok {
			return true
		}
	}
	return false
}

func ParseBpsLimitFromAnnotations(annotations map[string]string, defaultReadBps, defaultWriteBps int, prefix string) (int, int) {
	readBps, writeBps := defaultReadBps, defaultWriteBps
	annotationPrefix := prefix + "/"

	if val, ok := annotations[annotationPrefix+annotationkeys.RemovedAnnotationKey]; ok && val == "true" {
		return 0, 0
	}

	useSmart := hasSmartLimitAnnotation(annotations, prefix)

	if useSmart {
		if bps, ok := annotations[annotationPrefix+annotationkeys.BpsAnnotationKey]; ok {
			if value, err := units.RAMInBytes(bps); err == nil {
				return int(value), int(value)
			}
		}
		if rbps, ok := annotations[annotationPrefix+annotationkeys.ReadBpsAnnotationKey]; ok {
			if value, err := units.RAMInBytes(rbps); err == nil {
				readBps = int(value)
			}
		}
		if wbps, ok := annotations[annotationPrefix+annotationkeys.WriteBpsAnnotationKey]; ok {
			if value, err := units.RAMInBytes(wbps); err == nil {
				writeBps = int(value)
			}
		}
		return readBps, writeBps
	}

	// Fallback to legacy only if no smart limit annotations are present
	if bps, ok := annotations[annotationkeys.LegacyBpsAnnotationKey]; ok {
		if value, err := units.RAMInBytes(bps); err == nil {
			return int(value), int(value)
		}
	}
	if rbps, ok := annotations[annotationkeys.LegacyReadBpsAnnotationKey]; ok {
		if value, err := units.RAMInBytes(rbps); err == nil {
			readBps = int(value)
		}
	}
	if wbps, ok := annotations[annotationkeys.LegacyWriteBpsAnnotationKey]; ok {
		if value, err := units.RAMInBytes(wbps); err == nil {
			writeBps = int(value)
		}
	}

	return readBps, writeBps
}

type PodAnnotationState struct {
	Annotations map[string]string
	ReadIops    int
	WriteIops   int
	ReadBps     int
	WriteBps    int
}
