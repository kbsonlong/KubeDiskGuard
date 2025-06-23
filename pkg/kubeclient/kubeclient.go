package kubeclient

import (
	"context"
	"fmt"
	"os"
	"time"

	"KubeDiskGuard/pkg/cadvisor"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeClient 封装k8s client-go和kubelet API
// 支持in-cluster、kubeconfig、kubelet API
// 支持自定义CA、客户端证书、Token
type KubeClient struct {
	Clientset         *kubernetes.Clientset
	NodeName          string
	KubeletHost       string
	KubeletPort       string
	KubeletSkipVerify bool
	KubeletCAPath     string
	KubeletClientCert string
	KubeletClientKey  string
	KubeletTokenPath  string
	SATokenPath       string
	cadvisorCalc      *cadvisor.Calculator
}

// IKubeClient 接口，便于mock
type IKubeClient interface {
	ListNodePodsWithKubeletFirst() ([]corev1.Pod, error)
	WatchNodePods() (watch.Interface, error)
	GetPod(namespace, name string) (*corev1.Pod, error)
	UpdatePod(pod *corev1.Pod) (*corev1.Pod, error)
	GetNodeSummary() (*NodeSummary, error)
	GetCadvisorMetrics() (string, error)
	ParseCadvisorMetrics(metrics string) (*cadvisor.CadvisorMetrics, error)
	GetCadvisorIORate(containerID string, window time.Duration) (*cadvisor.IORate, error)
	GetCadvisorAverageIORate(containerID string, windows []time.Duration) (*cadvisor.IORate, error)
	CleanupCadvisorData(maxAge time.Duration)
	GetCadvisorStats() (containerCount, dataPointCount int)
	ConvertCadvisorToIOStats(metrics *cadvisor.CadvisorMetrics, containerID string) *IOStats
}

// 确保KubeClient实现IKubeClient
var _ IKubeClient = (*KubeClient)(nil)

// NewKubeClient 创建KubeClient，nodeName必须由参数传入
func NewKubeClient(nodeName, kubeconfigPath string) (*KubeClient, error) {
	if nodeName == "" {
		return nil, fmt.Errorf("nodeName is required, please set NODE_NAME env")
	}
	var config *rest.Config
	var err error

	if kubeconfigPath != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig: %v", err)
		}
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			// fallback to KUBECONFIG env
			if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
				config, err = clientcmd.BuildConfigFromFlags("", envPath)
				if err != nil {
					return nil, fmt.Errorf("failed to load kubeconfig from env: %v", err)
				}
			} else {
				return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
			}
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	kubeletHost := os.Getenv("KUBELET_HOST")
	if kubeletHost == "" {
		kubeletHost = "localhost"
	}
	kubeletPort := os.Getenv("KUBELET_PORT")
	if kubeletPort == "" {
		kubeletPort = "10250"
	}
	kubeletSkipVerify := os.Getenv("KUBELET_SKIP_VERIFY") == "true"
	// ServiceAccount Token 路径
	saTokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	if v := os.Getenv("KUBELET_SA_TOKEN_PATH"); v != "" {
		saTokenPath = v
	}
	caPath := "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	if v := os.Getenv("KUBELET_CA_PATH"); v != "" {
		caPath = v
	}

	// 新增：支持自定义CA、客户端证书、Token
	clientCert := os.Getenv("KUBELET_CLIENT_CERT_PATH")
	clientKey := os.Getenv("KUBELET_CLIENT_KEY_PATH")
	tokenPath := os.Getenv("KUBELET_TOKEN_PATH")

	return &KubeClient{
		Clientset:         clientset,
		NodeName:          nodeName,
		KubeletHost:       kubeletHost,
		KubeletPort:       kubeletPort,
		KubeletSkipVerify: kubeletSkipVerify,
		KubeletCAPath:     caPath,
		KubeletClientCert: clientCert,
		KubeletClientKey:  clientKey,
		KubeletTokenPath:  tokenPath,
		SATokenPath:       saTokenPath,
		cadvisorCalc:      cadvisor.NewCalculator(),
	}, nil
}

// ListNodePods 获取本节点所有Pod（API Server）
func (k *KubeClient) ListNodePods() ([]corev1.Pod, error) {
	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", k.NodeName).String()
	pods, err := k.Clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods from API Server: %v", err)
	}
	return pods.Items, nil
}

// ListNodePodsWithKubeletFirst 优先kubelet，失败fallback API Server
func (k *KubeClient) ListNodePodsWithKubeletFirst() ([]corev1.Pod, error) {
	pods, err := k.GetNodePodsFromKubelet()
	if err == nil {
		return pods, nil
	}
	return k.ListNodePods()
}

// WatchNodePods 监听本节点Pod事件
func (k *KubeClient) WatchNodePods() (watch.Interface, error) {
	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", k.NodeName).String()
	return k.Clientset.CoreV1().Pods("").Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
		Watch:         true,
	})
}

// GetPod 获取指定命名空间和名称的Pod
func (k *KubeClient) GetPod(namespace, name string) (*corev1.Pod, error) {
	pod, err := k.Clientset.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %v", err)
	}
	return pod, nil
}

// UpdatePod 更新指定Pod
func (k *KubeClient) UpdatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	pod, err := k.Clientset.CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update pod: %v", err)
	}
	return pod, nil
}
