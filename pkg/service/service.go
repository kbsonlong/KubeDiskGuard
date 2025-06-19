package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"time"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/detector"
	"iops-limit-service/pkg/runtime"

	"crypto/tls"

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
	// 获取本节点所有Pod信息
	pods, err := s.getNodePods()
	if err != nil {
		log.Printf("Failed to get node pods: %v", err)
		// 如果无法获取Pod信息，使用默认配置处理现有容器
		return s.processContainersWithDefaultLimit()
	}

	// 创建Pod注解映射
	podAnnotations := make(map[string]int)
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodRunning {
			key := pod.Namespace + "/" + pod.Name
			iopsLimit := ParseIopsLimitFromAnnotations(pod.Annotations, s.config.ContainerIOPSLimit)
			podAnnotations[key] = iopsLimit
		}
	}

	// 获取所有容器
	containers, err := s.runtime.GetContainers()
	if err != nil {
		return fmt.Errorf("failed to get containers: %v", err)
	}

	// 处理每个容器
	for _, container := range containers {
		// 尝试从容器注解中获取Pod信息
		podNamespace, podName := s.extractPodInfoFromContainer(container)
		if podNamespace != "" && podName != "" {
			key := podNamespace + "/" + podName
			if iopsLimit, exists := podAnnotations[key]; exists {
				// 使用Pod注解中的IOPS限制
				if err := s.runtime.SetIOPSLimit(container, iopsLimit); err != nil {
					log.Printf("Failed to set IOPS limit for container %s (pod: %s): %v", container.ID, key, err)
				} else {
					log.Printf("Applied Pod-specific IOPS limit for container %s (pod: %s): %d", container.ID, key, iopsLimit)
				}
				continue
			}
		}

		// 如果无法获取Pod信息或Pod不存在，使用默认配置
		if err := s.runtime.ProcessContainer(container); err != nil {
			log.Printf("Failed to process container %s with default limit: %v", container.ID, err)
		} else {
			log.Printf("Applied default IOPS limit for container %s: %d", container.ID, s.config.ContainerIOPSLimit)
		}
	}

	return nil
}

// getNodePods 获取本节点的所有Pod（优先使用kubelet API，fallback到API Server）
func (s *IOPSLimitService) getNodePods() ([]corev1.Pod, error) {
	// 首先尝试使用kubelet API
	pods, err := s.getNodePodsFromKubelet()
	if err == nil {
		log.Printf("Successfully got %d pods from kubelet API", len(pods))
		return pods, nil
	}

	log.Printf("Failed to get pods from kubelet API: %v, falling back to API Server", err)

	// 如果kubelet API失败，回退到API Server
	return s.getNodePodsFromAPIServer()
}

// getNodePodsFromKubelet 使用kubelet API获取本节点Pod信息
func (s *IOPSLimitService) getNodePodsFromKubelet() ([]corev1.Pod, error) {
	// 使用kubelet API获取本节点Pod信息
	kubeletHost := os.Getenv("KUBELET_HOST")
	if kubeletHost == "" {
		kubeletHost = "localhost"
	}

	kubeletPort := os.Getenv("KUBELET_PORT")
	if kubeletPort == "" {
		kubeletPort = "10250"
	}

	// 构建kubelet API URL
	kubeletURL := fmt.Sprintf("https://%s:%s/pods", kubeletHost, kubeletPort)

	// 创建HTTP客户端，跳过TLS验证（kubelet使用自签名证书）
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 10 * time.Second,
	}

	// 发送请求到kubelet API
	req, err := http.NewRequest("GET", kubeletURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// 设置必要的头部
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request kubelet API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kubelet API returned status: %d", resp.StatusCode)
	}

	// 解析响应
	var podList corev1.PodList
	if err := json.NewDecoder(resp.Body).Decode(&podList); err != nil {
		return nil, fmt.Errorf("failed to decode kubelet response: %v", err)
	}

	return podList.Items, nil
}

// getNodePodsFromAPIServer 使用API Server获取本节点Pod信息（fallback方法）
func (s *IOPSLimitService) getNodePodsFromAPIServer() ([]corev1.Pod, error) {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		nodeName, _ = os.Hostname()
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", nodeName).String()
	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods from API Server: %v", err)
	}

	return pods.Items, nil
}

// extractPodInfoFromContainer 从容器信息中提取Pod信息
func (s *IOPSLimitService) extractPodInfoFromContainer(container *container.ContainerInfo) (namespace, name string) {
	// 尝试从容器注解中获取Pod信息
	if namespace, ok := container.Annotations["io.kubernetes.pod.namespace"]; ok {
		if name, ok := container.Annotations["io.kubernetes.pod.name"]; ok {
			return namespace, name
		}
	}
	return "", ""
}

// processContainersWithDefaultLimit 使用默认配置处理现有容器（fallback方法）
func (s *IOPSLimitService) processContainersWithDefaultLimit() error {
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
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			old := podAnnotations[key]
			newAnn := pod.Annotations
			iopsLimit := ParseIopsLimitFromAnnotations(newAnn, s.config.ContainerIOPSLimit)
			if reflect.DeepEqual(old.Annotations, newAnn) && old.IopsLimit == iopsLimit {
				continue
			}
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
