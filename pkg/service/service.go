package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"KubeDiskGuard/pkg/annotationkeys"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/container"
	"KubeDiskGuard/pkg/detector"
	"KubeDiskGuard/pkg/kubeclient"
	"KubeDiskGuard/pkg/runtime"

	"github.com/docker/go-units"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
)

var (
	containerTotal   = prometheus.NewCounter(prometheus.CounterOpts{Name: "kubediskguard_container_total", Help: "处理的容器总数"})
	containerSuccess = prometheus.NewCounter(prometheus.CounterOpts{Name: "kubediskguard_container_success_total", Help: "成功设置限速的容器数"})
	containerFail    = prometheus.NewCounter(prometheus.CounterOpts{Name: "kubediskguard_container_fail_total", Help: "设置限速失败的容器数"})
	containerSkip    = prometheus.NewCounter(prometheus.CounterOpts{Name: "kubediskguard_container_skip_total", Help: "被跳过的容器数"})
	containerReset   = prometheus.NewCounter(prometheus.CounterOpts{Name: "kubediskguard_container_reset_total", Help: "被取消限速的容器数"})
	limitApplyTotal  = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kubediskguard_limit_apply_total", Help: "按策略来源和执行结果统计的限速下发次数"}, []string{"source", "result"})
	desiredLimit     = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "kubediskguard_container_desired_limit", Help: "最近一次下发的容器限额；resource 为 riops、wiops、rbps 或 wbps"}, []string{"namespace", "pod", "container", "resource", "source"})
	reconcileTotal   = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "kubediskguard_reconcile_total", Help: "周期性对账次数"}, []string{"result"})
)

func init() {
	prometheus.MustRegister(containerTotal, containerSuccess, containerFail, containerSkip, containerReset)
	prometheus.MustRegister(limitApplyTotal, desiredLimit, reconcileTotal)
}

// AppliedLimit 是最近一次成功执行的、容器级的静态 IO 策略快照。
type AppliedLimit struct {
	ContainerID string    `json:"container_id"`
	Container   string    `json:"container"`
	PodName     string    `json:"pod_name"`
	Namespace   string    `json:"namespace"`
	ReadIOPS    int       `json:"read_iops"`
	WriteIOPS   int       `json:"write_iops"`
	ReadBPS     int       `json:"read_bps"`
	WriteBPS    int       `json:"write_bps"`
	Source      string    `json:"source"`
	AppliedAt   time.Time `json:"applied_at"`
}

// ServiceStatus 用于 readiness 和运维 API，反映最近一次全量对账结果。
type ServiceStatus struct {
	LastReconcileAt time.Time `json:"last_reconcile_at"`
	LastError       string    `json:"last_error,omitempty"`
	ManagedCount    int       `json:"managed_count"`
}

// KubeDiskGuardService 是节点级、声明式磁盘 IO 限速执行器。
type KubeDiskGuardService struct {
	Config     *config.Config
	runtime    container.Runtime
	kubeClient kubeclient.IKubeClient

	mu              sync.RWMutex
	applied         map[string]AppliedLimit
	lastReconcileAt time.Time
	lastError       string
}

func NewKubeDiskGuardService(cfg *config.Config) (*KubeDiskGuardService, error) {
	service := &KubeDiskGuardService{Config: cfg, applied: make(map[string]AppliedLimit)}
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
	service.kubeClient, err = kubeclient.NewKubeClientWithConfig(nodeName, cfg.KubeConfigPath, cfg)
	if err != nil {
		_ = service.runtime.Close()
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
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

func (s *KubeDiskGuardService) processPodContainers(pod corev1.Pod) (map[string]struct{}, bool) {
	active := make(map[string]struct{})
	hadFailure := false
	readIOPS, writeIOPS := ParseIopsLimitFromAnnotations(pod.Annotations, s.Config.ContainerReadIOPSLimit, s.Config.ContainerWriteIOPSLimit, s.Config.AnnotationPrefix)
	readBPS, writeBPS := ParseBpsLimitFromAnnotations(pod.Annotations, s.Config.ContainerReadBPSLimit, s.Config.ContainerWriteBPSLimit, s.Config.AnnotationPrefix)
	source := PolicySource(pod.Annotations, s.Config.AnnotationPrefix)

	for _, cs := range pod.Status.ContainerStatuses {
		containerID := parseRuntimeID(cs.ContainerID)
		if containerID == "" {
			continue
		}
		active[containerID] = struct{}{}
		containerTotal.Inc()
		containerInfo, err := s.runtime.GetContainerByID(containerID)
		if err != nil {
			hadFailure = true
			s.recordFailure(containerID, err)
			log.Printf("Failed to get container info for %s: %v", containerID, err)
			containerFail.Inc()
			limitApplyTotal.WithLabelValues(source, "error").Inc()
			continue
		}
		if s.ShouldSkipContainer(containerInfo.Image, containerInfo.Name) {
			log.Printf("Skip IO limit for container %s (excluded by keyword)", containerInfo.ID)
			containerSkip.Inc()
			continue
		}

		if readIOPS == 0 && writeIOPS == 0 && readBPS == 0 && writeBPS == 0 {
			err = s.runtime.ResetLimits(containerInfo)
			if err == nil {
				containerReset.Inc()
				s.removeApplied(containerID)
				limitApplyTotal.WithLabelValues(source, "reset").Inc()
			} else {
				hadFailure = true
				s.recordFailure(containerID, err)
				containerFail.Inc()
				limitApplyTotal.WithLabelValues(source, "error").Inc()
				log.Printf("Failed to reset limits for container %s: %v", containerInfo.ID, err)
			}
			continue
		}
		if err := s.runtime.SetLimits(containerInfo, readIOPS, writeIOPS, readBPS, writeBPS); err != nil {
			hadFailure = true
			s.recordFailure(containerID, err)
			log.Printf("Failed to set limits for container %s: %v", containerInfo.ID, err)
			containerFail.Inc()
			limitApplyTotal.WithLabelValues(source, "error").Inc()
			continue
		}
		containerSuccess.Inc()
		limitApplyTotal.WithLabelValues(source, "success").Inc()
		s.recordApplied(AppliedLimit{ContainerID: containerID, Container: cs.Name, PodName: pod.Name, Namespace: pod.Namespace, ReadIOPS: readIOPS, WriteIOPS: writeIOPS, ReadBPS: readBPS, WriteBPS: writeBPS, Source: source, AppliedAt: time.Now()})
	}
	return active, hadFailure
}

// Reconcile 重新读取本节点所有 Pod，并确保策略在容器重建和 Agent 重启后再次生效。
func (s *KubeDiskGuardService) Reconcile() error {
	pods, err := s.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		s.setReconcileResult(err)
		reconcileTotal.WithLabelValues("error").Inc()
		return fmt.Errorf("list node pods: %w", err)
	}
	active := make(map[string]struct{})
	hadFailure := false
	for _, pod := range pods {
		if !s.ShouldProcessPod(pod) {
			continue
		}
		podActive, podFailed := s.processPodContainers(pod)
		hadFailure = hadFailure || podFailed
		for containerID := range podActive {
			active[containerID] = struct{}{}
		}
	}
	s.pruneApplied(active)
	if hadFailure {
		err := fmt.Errorf("one or more container policies failed to apply")
		s.setReconcileResult(err)
		reconcileTotal.WithLabelValues("error").Inc()
		return err
	}
	s.setReconcileResult(nil)
	reconcileTotal.WithLabelValues("success").Inc()
	return nil
}

// Run combines event-driven application with periodic full reconciliation.
func (s *KubeDiskGuardService) Run(ctx context.Context) error {
	if err := s.Reconcile(); err != nil {
		log.Printf("Initial reconcile failed: %v", err)
	}
	interval := time.Duration(s.Config.ReconcileInterval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		watcher, err := s.kubeClient.WatchNodePods()
		if err != nil {
			log.Printf("Pod watch failed; retrying: %v", err)
			if !waitForRetry(ctx, 5*time.Second) {
				return nil
			}
			continue
		}
		if err := s.consumeWatch(ctx, watcher, ticker.C); err != nil && ctx.Err() == nil {
			log.Printf("Pod watch ended; recreating: %v", err)
		}
		watcher.Stop()
		if ctx.Err() != nil {
			return nil
		}
	}
}

func (s *KubeDiskGuardService) consumeWatch(ctx context.Context, watcher watch.Interface, tick <-chan time.Time) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			if err := s.Reconcile(); err != nil {
				log.Printf("Periodic reconcile failed: %v", err)
			}
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok || event.Type == watch.Deleted || !s.ShouldProcessPod(*pod) {
				continue
			}
			s.processPodContainers(*pod)
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *KubeDiskGuardService) ResetAllContainersIOPSLimit() error {
	pods, err := s.kubeClient.ListNodePodsWithKubeletFirst()
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
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
			if err == nil {
				err = s.runtime.ResetLimits(containerInfo)
			}
			if err != nil {
				return fmt.Errorf("reset container %s: %w", containerID, err)
			}
			s.removeApplied(containerID)
		}
	}
	return nil
}

func (s *KubeDiskGuardService) Close() error { return s.runtime.Close() }

// GetAppliedLimits returns a copy, sorted for stable API output.
func (s *KubeDiskGuardService) GetAppliedLimits() []AppliedLimit {
	s.mu.RLock()
	limits := make([]AppliedLimit, 0, len(s.applied))
	for _, limit := range s.applied {
		limits = append(limits, limit)
	}
	s.mu.RUnlock()
	sort.Slice(limits, func(i, j int) bool {
		if limits[i].Namespace == limits[j].Namespace {
			if limits[i].PodName == limits[j].PodName {
				return limits[i].Container < limits[j].Container
			}
			return limits[i].PodName < limits[j].PodName
		}
		return limits[i].Namespace < limits[j].Namespace
	})
	return limits
}

func (s *KubeDiskGuardService) GetServiceStatus() ServiceStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ServiceStatus{LastReconcileAt: s.lastReconcileAt, LastError: s.lastError, ManagedCount: len(s.applied)}
}

func (s *KubeDiskGuardService) recordApplied(limit AppliedLimit) {
	s.mu.Lock()
	if previous, exists := s.applied[limit.ContainerID]; exists {
		deleteDesiredLimit(previous)
	}
	s.applied[limit.ContainerID] = limit
	s.mu.Unlock()
	for resource, value := range map[string]int{"riops": limit.ReadIOPS, "wiops": limit.WriteIOPS, "rbps": limit.ReadBPS, "wbps": limit.WriteBPS} {
		desiredLimit.WithLabelValues(limit.Namespace, limit.PodName, limit.Container, resource, limit.Source).Set(float64(value))
	}
}

func (s *KubeDiskGuardService) removeApplied(containerID string) {
	s.mu.Lock()
	if limit, exists := s.applied[containerID]; exists {
		deleteDesiredLimit(limit)
	}
	delete(s.applied, containerID)
	s.mu.Unlock()
}

func (s *KubeDiskGuardService) recordFailure(containerID string, err error) {
	s.mu.Lock()
	s.lastError = fmt.Sprintf("container %s: %v", containerID, err)
	s.mu.Unlock()
}

func (s *KubeDiskGuardService) pruneApplied(active map[string]struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for containerID := range s.applied {
		if _, exists := active[containerID]; !exists {
			deleteDesiredLimit(s.applied[containerID])
			delete(s.applied, containerID)
		}
	}
}

func deleteDesiredLimit(limit AppliedLimit) {
	for _, resource := range []string{"riops", "wiops", "rbps", "wbps"} {
		desiredLimit.DeleteLabelValues(limit.Namespace, limit.PodName, limit.Container, resource, limit.Source)
	}
}

func (s *KubeDiskGuardService) setReconcileResult(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastReconcileAt = time.Now()
	if err == nil {
		s.lastError = ""
	} else {
		s.lastError = err.Error()
	}
}

func parseRuntimeID(k8sID string) string {
	for _, prefix := range []string{"docker://", "containerd://", "cri-o://"} {
		if strings.HasPrefix(k8sID, prefix) {
			return strings.TrimPrefix(k8sID, prefix)
		}
	}
	return k8sID
}

// PolicySource reports the effective precedence tier for an annotation set.
func PolicySource(annotations map[string]string, prefix string) string {
	if annotations[prefix+"/"+annotationkeys.RemovedAnnotationKey] == "true" || hasAnnotationPolicy(annotations, prefix) {
		return "annotation"
	}
	for _, key := range []string{annotationkeys.LegacyIopsAnnotationKey, annotationkeys.LegacyReadIopsAnnotationKey, annotationkeys.LegacyWriteIopsAnnotationKey, annotationkeys.LegacyBpsAnnotationKey, annotationkeys.LegacyReadBpsAnnotationKey, annotationkeys.LegacyWriteBpsAnnotationKey} {
		if _, exists := annotations[key]; exists {
			return "legacy_annotation"
		}
	}
	return "default"
}

func ParseIopsLimitFromAnnotations(annotations map[string]string, defaultReadIOPS, defaultWriteIOPS int, prefix string) (int, int) {
	readIOPS, writeIOPS := defaultReadIOPS, defaultWriteIOPS
	annotationPrefix := prefix + "/"
	if annotations[annotationPrefix+annotationkeys.RemovedAnnotationKey] == "true" {
		return 0, 0
	}
	if hasAnnotationPolicy(annotations, prefix) {
		if value, err := strconv.Atoi(annotations[annotationPrefix+annotationkeys.IopsAnnotationKey]); err == nil {
			return value, value
		}
		if value, err := strconv.Atoi(annotations[annotationPrefix+annotationkeys.ReadIopsAnnotationKey]); err == nil {
			readIOPS = value
		}
		if value, err := strconv.Atoi(annotations[annotationPrefix+annotationkeys.WriteIopsAnnotationKey]); err == nil {
			writeIOPS = value
		}
		return readIOPS, writeIOPS
	}
	if value, err := strconv.Atoi(annotations[annotationkeys.LegacyIopsAnnotationKey]); err == nil {
		return value, value
	}
	if value, err := strconv.Atoi(annotations[annotationkeys.LegacyReadIopsAnnotationKey]); err == nil {
		readIOPS = value
	}
	if value, err := strconv.Atoi(annotations[annotationkeys.LegacyWriteIopsAnnotationKey]); err == nil {
		writeIOPS = value
	}
	return readIOPS, writeIOPS
}

func ParseBpsLimitFromAnnotations(annotations map[string]string, defaultReadBPS, defaultWriteBPS int, prefix string) (int, int) {
	readBPS, writeBPS := defaultReadBPS, defaultWriteBPS
	annotationPrefix := prefix + "/"
	if annotations[annotationPrefix+annotationkeys.RemovedAnnotationKey] == "true" {
		return 0, 0
	}
	if hasAnnotationPolicy(annotations, prefix) {
		if value, err := units.RAMInBytes(annotations[annotationPrefix+annotationkeys.BpsAnnotationKey]); err == nil {
			return int(value), int(value)
		}
		if value, err := units.RAMInBytes(annotations[annotationPrefix+annotationkeys.ReadBpsAnnotationKey]); err == nil {
			readBPS = int(value)
		}
		if value, err := units.RAMInBytes(annotations[annotationPrefix+annotationkeys.WriteBpsAnnotationKey]); err == nil {
			writeBPS = int(value)
		}
		return readBPS, writeBPS
	}
	if value, err := units.RAMInBytes(annotations[annotationkeys.LegacyBpsAnnotationKey]); err == nil {
		return int(value), int(value)
	}
	if value, err := units.RAMInBytes(annotations[annotationkeys.LegacyReadBpsAnnotationKey]); err == nil {
		readBPS = int(value)
	}
	if value, err := units.RAMInBytes(annotations[annotationkeys.LegacyWriteBpsAnnotationKey]); err == nil {
		writeBPS = int(value)
	}
	return readBPS, writeBPS
}

func hasAnnotationPolicy(annotations map[string]string, prefix string) bool {
	for _, key := range []string{annotationkeys.IopsAnnotationKey, annotationkeys.ReadIopsAnnotationKey, annotationkeys.WriteIopsAnnotationKey, annotationkeys.BpsAnnotationKey, annotationkeys.ReadBpsAnnotationKey, annotationkeys.WriteBpsAnnotationKey} {
		if _, exists := annotations[prefix+"/"+key]; exists {
			return true
		}
	}
	return false
}

// NewKubeDiskGuardServiceWithKubeClient is a constructor for tests with a mock Kubernetes client.
func NewKubeDiskGuardServiceWithKubeClient(cfg *config.Config, kubeClient kubeclient.IKubeClient) (*KubeDiskGuardService, error) {
	service := &KubeDiskGuardService{Config: cfg, kubeClient: kubeClient, applied: make(map[string]AppliedLimit)}
	var err error
	service.runtime, err = runtime.NewDockerRuntime(cfg)
	if err != nil {
		return nil, err
	}
	return service, nil
}
