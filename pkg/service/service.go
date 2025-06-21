package service

import (
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"

	"KubeDiskGuard/pkg/cgroup"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/detector"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/kubelet"
	"KubeDiskGuard/pkg/runtime"
	"KubeDiskGuard/pkg/smartlimit"

	"github.com/docker/go-units"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
)

const (
	// Smart limit annotation keys
	RemovedAnnotationKey   = "limit-removed"
	IopsAnnotationKey      = "iops-limit"
	ReadIopsAnnotationKey  = "read-iops-limit"
	WriteIopsAnnotationKey = "write-iops-limit"
	BpsAnnotationKey       = "bps-limit"
	ReadBpsAnnotationKey   = "read-bps-limit"
	WriteBpsAnnotationKey  = "write-bps-limit"

	// Legacy nvme annotation keys
	LegacyIopsAnnotationKey      = "nvme-iops-limit"
	LegacyReadIopsAnnotationKey  = "nvme-iops-read-limit"
	LegacyWriteIopsAnnotationKey = "nvme-iops-write-limit"
	LegacyBpsAnnotationKey       = "nvme-bps-limit"
	LegacyReadBpsAnnotationKey   = "nvme-bps-read-limit"
	LegacyWriteBpsAnnotationKey  = "nvme-bps-write-limit"
)

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

	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		return nil, fmt.Errorf("NODE_NAME env is required")
	}
	kubeClient, err := kubeclient.NewKubeClient(nodeName, cfg.KubeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeclient: %v", err)
	}
	service.kubeClient = kubeClient

	if cfg.SmartLimitEnabled {
		cgroupMgr := cgroup.NewManager(cfg.CgroupVersion)
		var kubeletClient *kubelet.KubeletClient
		if cfg.SmartLimitUseKubeletAPI && cfg.KubeletHost != "" && cfg.KubeletPort != "" {
			kubeletClient, err = kubelet.NewKubeletClient(cfg.KubeletHost, cfg.KubeletPort, cfg.KubeletTokenPath, cfg.KubeletCAPath, cfg.KubeletSkipVerify)
			if err != nil {
				log.Printf("Failed to create kubelet client: %v", err)
			} else {
				log.Printf("Kubelet client initialized for host: %s:%s", cfg.KubeletHost, cfg.KubeletPort)
			}
		}
		service.smartLimit = smartlimit.NewSmartLimitManager(cfg, service.kubeClient, kubeletClient, cgroupMgr)
		log.Printf("Smart limit manager initialized")
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
		containerInfo, err := s.runtime.GetContainerByID(containerID)
		if err != nil {
			log.Printf("Failed to get container info for %s: %v", containerID, err)
			continue
		}

		if s.ShouldSkipContainer(containerInfo.Image, containerInfo.Name) {
			continue
		}

		if readIopsVal == 0 && writeIopsVal == 0 && readBps == 0 && writeBps == 0 {
			if err := s.runtime.ResetLimits(containerInfo); err != nil {
				log.Printf("Failed to reset all limits for container %s: %v", containerInfo.ID, err)
			} else {
				log.Printf("Successfully reset all limits for container %s", containerInfo.ID)
			}
			continue
		}

		if err := s.runtime.SetLimits(containerInfo, readIopsVal, writeIopsVal, readBps, writeBps); err != nil {
			log.Printf("Failed to set limits for container %s: %v", containerInfo.ID, err)
		} else {
			log.Printf("Successfully set limits for container %s: riops=%d, wiops=%d, rbps=%d, wbps=%d", containerInfo.ID, readIopsVal, writeIopsVal, readBps, writeBps)
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

func (s *KubeDiskGuardService) watchPodEvents(watcher watch.Interface, podAnnotations map[string]PodAnnotationState) {
	for event := range watcher.ResultChan() {
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}

		switch event.Type {
		case watch.Added, watch.Modified:
			if !s.ShouldProcessPod(*pod) {
				continue
			}

			key := pod.Namespace + "/" + pod.Name
			old, exists := podAnnotations[key]
			newAnn := pod.Annotations
			readIops, writeIops := ParseIopsLimitFromAnnotations(newAnn, s.Config.ContainerReadIOPSLimit, s.Config.ContainerWriteIOPSLimit, s.Config.SmartLimitAnnotationPrefix)
			readBps, writeBps := ParseBpsLimitFromAnnotations(newAnn, s.Config.ContainerReadBPSLimit, s.Config.ContainerWriteBPSLimit, s.Config.SmartLimitAnnotationPrefix)

			if exists && reflect.DeepEqual(old.Annotations, newAnn) &&
				old.ReadIOPS == readIops && old.WriteIOPS == writeIops &&
				old.ReadBPS == readBps && old.WriteBPS == writeBps {
				continue
			}

			s.processPodContainers(*pod)
			podAnnotations[key] = PodAnnotationState{
				Annotations: newAnn,
				ReadIOPS:    readIops,
				WriteIOPS:   writeIops,
				ReadBPS:     readBps,
				WriteBPS:    writeBps,
			}
		}
	}
}

func (s *KubeDiskGuardService) Close() error {
	if s.smartLimit != nil {
		s.smartLimit.Stop()
	}
	if s.runtime != nil {
		return s.runtime.Close()
	}
	return nil
}

func (s *KubeDiskGuardService) Run() error {
	if s.smartLimit != nil {
		s.smartLimit.Start()
	}

	pods, err := s.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
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
			ReadIOPS:    readIops,
			WriteIOPS:   writeIops,
			ReadBPS:     readBps,
			WriteBPS:    writeBps,
		}
	}

	watcher, err := s.kubeClient.WatchNodePods()
	if err != nil {
		return fmt.Errorf("failed to watch pods: %v", err)
	}
	defer watcher.Stop()

	log.Println("Start watching pod events...")
	s.watchPodEvents(watcher, podAnnotations)
	return nil
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

func NewKubeDiskGuardServiceWithKubeClient(cfg *config.Config, kc kubeclient.IKubeClient) (*KubeDiskGuardService, error) {
	service := &KubeDiskGuardService{
		Config:     cfg,
		kubeClient: kc,
	}
	var err error
	switch cfg.ContainerRuntime {
	case "docker":
		service.runtime, err = runtime.NewDockerRuntime(cfg)
	case "containerd":
		service.runtime, err = runtime.NewContainerdRuntime(cfg)
	default:
		service.runtime, err = runtime.NewDockerRuntime(cfg) // default
	}
	if err != nil {
		return nil, err
	}
	return service, nil
}

func hasSmartLimitAnnotation(annotations map[string]string, prefix string) bool {
	annotationPrefix := prefix + "/"
	smartKeys := []string{
		IopsAnnotationKey, ReadIopsAnnotationKey, WriteIopsAnnotationKey,
		BpsAnnotationKey, ReadBpsAnnotationKey, WriteBpsAnnotationKey,
	}
	for _, key := range smartKeys {
		if _, ok := annotations[annotationPrefix+key]; ok {
			return true
		}
	}
	return false
}

func ParseIopsLimitFromAnnotations(annotations map[string]string, defaultReadIops, defaultWriteIops int, prefix string) (int, int) {
	readIops, writeIops := defaultReadIops, defaultWriteIops
	annotationPrefix := prefix + "/"

	if val, ok := annotations[annotationPrefix+RemovedAnnotationKey]; ok && val == "true" {
		return 0, 0
	}

	useSmart := hasSmartLimitAnnotation(annotations, prefix)

	if useSmart {
		if iops, ok := annotations[annotationPrefix+IopsAnnotationKey]; ok {
			if value, err := strconv.Atoi(iops); err == nil {
				return value, value
			}
		}
		if riops, ok := annotations[annotationPrefix+ReadIopsAnnotationKey]; ok {
			if value, err := strconv.Atoi(riops); err == nil {
				readIops = value
			}
		}
		if wiops, ok := annotations[annotationPrefix+WriteIopsAnnotationKey]; ok {
			if value, err := strconv.Atoi(wiops); err == nil {
				writeIops = value
			}
		}
		return readIops, writeIops
	}

	// Fallback to legacy only if no smart limit annotations are present
	if iops, ok := annotations[LegacyIopsAnnotationKey]; ok {
		if value, err := strconv.Atoi(iops); err == nil {
			return value, value
		}
	}
	if riops, ok := annotations[LegacyReadIopsAnnotationKey]; ok {
		if value, err := strconv.Atoi(riops); err == nil {
			readIops = value
		}
	}
	if wiops, ok := annotations[LegacyWriteIopsAnnotationKey]; ok {
		if value, err := strconv.Atoi(wiops); err == nil {
			writeIops = value
		}
	}

	return readIops, writeIops
}

func ParseBpsLimitFromAnnotations(annotations map[string]string, defaultReadBps, defaultWriteBps int, prefix string) (int, int) {
	readBps, writeBps := defaultReadBps, defaultWriteBps
	annotationPrefix := prefix + "/"

	if val, ok := annotations[annotationPrefix+RemovedAnnotationKey]; ok && val == "true" {
		return 0, 0
	}

	useSmart := hasSmartLimitAnnotation(annotations, prefix)

	if useSmart {
		if bps, ok := annotations[annotationPrefix+BpsAnnotationKey]; ok {
			if value, err := units.RAMInBytes(bps); err == nil {
				return int(value), int(value)
			}
		}
		if rbps, ok := annotations[annotationPrefix+ReadBpsAnnotationKey]; ok {
			if value, err := units.RAMInBytes(rbps); err == nil {
				readBps = int(value)
			}
		}
		if wbps, ok := annotations[annotationPrefix+WriteBpsAnnotationKey]; ok {
			if value, err := units.RAMInBytes(wbps); err == nil {
				writeBps = int(value)
			}
		}
		return readBps, writeBps
	}

	// Fallback to legacy only if no smart limit annotations are present
	if bps, ok := annotations[LegacyBpsAnnotationKey]; ok {
		if value, err := units.RAMInBytes(bps); err == nil {
			return int(value), int(value)
		}
	}
	if rbps, ok := annotations[LegacyReadBpsAnnotationKey]; ok {
		if value, err := units.RAMInBytes(rbps); err == nil {
			readBps = int(value)
		}
	}
	if wbps, ok := annotations[LegacyWriteBpsAnnotationKey]; ok {
		if value, err := units.RAMInBytes(wbps); err == nil {
			writeBps = int(value)
		}
	}

	return readBps, writeBps
}

func parseRuntimeID(containerID string) string {
	parts := strings.Split(containerID, "://")
	if len(parts) == 2 {
		return parts[1]
	}
	return containerID
}

type PodAnnotationState struct {
	Annotations map[string]string
	ReadIOPS    int
	WriteIOPS   int
	ReadBPS     int
	WriteBPS    int
}
