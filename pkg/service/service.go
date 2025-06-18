package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"reflect"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/detector"
	"iops-limit-service/pkg/runtime"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// IOPSLimitService IOPS限制服务
type IOPSLimitService struct {
	config  *config.Config
	runtime container.Runtime
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
	if config.ContainerRuntime == "docker" {
		service.runtime, err = runtime.NewDockerRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker runtime: %v", err)
		}
	} else if config.ContainerRuntime == "containerd" {
		service.runtime, err = runtime.NewContainerdRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create containerd runtime: %v", err)
		}
	} else {
		return nil, fmt.Errorf("unsupported container runtime: %s", config.ContainerRuntime)
	}

	return service, nil
}

// ProcessExistingContainers 处理现有容器
func (s *IOPSLimitService) ProcessExistingContainers() error {
	containers, err := s.runtime.GetContainers()
	if err != nil {
		return fmt.Errorf("failed to get containers: %v", err)
	}

	for _, container := range containers {
		if err := s.runtime.ProcessContainer(container); err != nil {
			log.Printf("Failed to process container %s: %v", container.ID, err)
		}
	}

	return nil
}

// WatchEvents 监听事件
func (s *IOPSLimitService) WatchEvents() error {
	return s.WatchPodEvents()
}

// WatchPodEvents 监听本节点Pod变化，动态调整IOPS
func (s *IOPSLimitService) WatchPodEvents() error {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		nodeName, _ = os.Hostname()
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	watcher, err := clientset.CoreV1().Pods("").Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
		Watch:         true,
	})
	if err != nil {
		return err
	}
	log.Printf("Start watching pods on node: %s", nodeName)
	podAnnotations := make(map[string]map[string]string)
	for event := range watcher.ResultChan() {
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}
		key := pod.Namespace + "/" + pod.Name
		switch event.Type {
		case watch.Added, watch.Modified:
			oldAnn := podAnnotations[key]
			newAnn := pod.Annotations
			if !reflect.DeepEqual(oldAnn, newAnn) {
				iopsLimit := ParseIopsLimitFromAnnotations(newAnn, s.config.ContainerIOPSLimit)
				containers, err := s.runtime.GetContainersByPod(pod.Namespace, pod.Name)
				if err != nil {
					log.Printf("GetContainersByPod error: %v", err)
					continue
				}
				for _, c := range containers {
					if container.ShouldSkip(c, s.config.ExcludeKeywords, s.config.ExcludeNamespaces, s.config.ExcludeRegexps, s.config.ExcludeLabelSelector) {
						log.Printf("Skip IOPS limit for container %s (excluded by filter)", c.ID)
						continue
					}
					if err := s.runtime.SetIOPSLimit(c, iopsLimit); err != nil {
						log.Printf("SetIOPSLimit failed for %s: %v", c.ID, err)
					}
				}
				podAnnotations[key] = copyMap(newAnn)
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
