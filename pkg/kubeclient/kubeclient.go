package kubeclient

import (
	"context"
	"fmt"
	"os"
	"time"

	"KubeDiskGuard/pkg/cadvisor"
	"KubeDiskGuard/pkg/config"

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
	KubeletServerName string
	SATokenPath       string
	RestConfig        *rest.Config // 保存 kubeconfig 配置，用于提取认证信息
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
	TestKubeletConnection() error
	ParseCadvisorMetrics(metrics string) (*cadvisor.CadvisorMetrics, error)
	GetCadvisorIORate(containerID string, window time.Duration) (*cadvisor.IORate, error)
	GetCadvisorAverageIORate(containerID string, windows []time.Duration) (*cadvisor.IORate, error)
	CleanupCadvisorData(maxAge time.Duration)
	GetCadvisorStats() (containerCount, dataPointCount int)
	ConvertCadvisorToIOStats(metrics *cadvisor.CadvisorMetrics, containerID string) *IOStats
	CreateEvent(namespace, podName, eventType, reason, message string) error
}

// 确保KubeClient实现IKubeClient
var _ IKubeClient = (*KubeClient)(nil)

// NewKubeClientWithConfig 创建KubeClient，使用配置参数
func NewKubeClientWithConfig(nodeName, kubeconfigPath string, cfg *config.Config) (*KubeClient, error) {
	if nodeName == "" {
		return nil, fmt.Errorf("nodeName is required, please set NODE_NAME env")
	}
	
	var restConfig *rest.Config
	var clientset *kubernetes.Clientset
	var err error

	// 当使用 kubelet API 模式时，跳过 Kubernetes 客户端创建
	if !cfg.SmartLimitUseKubeletAPI {
		if kubeconfigPath != "" {
			restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load kubeconfig: %v", err)
			}
		} else {
			restConfig, err = rest.InClusterConfig()
			if err != nil {
				// fallback to KUBECONFIG env
				if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
					restConfig, err = clientcmd.BuildConfigFromFlags("", envPath)
					if err != nil {
						return nil, fmt.Errorf("failed to load kubeconfig from env: %v", err)
					}
				} else {
					return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
				}
			}
		}

		clientset, err = kubernetes.NewForConfig(restConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
		}
	} else {
		// 使用 kubelet API 模式，不需要 Kubernetes 客户端
		fmt.Printf("Using kubelet API mode, skipping Kubernetes client creation\n")
	}

	// 使用配置参数，如果为空则使用默认值
	kubeletHost := cfg.KubeletHost
	if kubeletHost == "" {
		kubeletHost = "localhost"
	}
	kubeletPort := cfg.KubeletPort
	if kubeletPort == "" {
		kubeletPort = "10250"
	}

	// ServiceAccount Token 路径
	saTokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	if cfg.KubeletTokenPath != "" {
		saTokenPath = cfg.KubeletTokenPath
	}
	caPath := "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	if cfg.KubeletCAPath != "" {
		caPath = cfg.KubeletCAPath
	}

	// 证书配置
	var clientCertPath, clientKeyPath string
	if !cfg.SmartLimitUseKubeletAPI && restConfig != nil {
		// 从 kubeconfig 中提取客户端证书信息
		if restConfig.CertFile != "" && restConfig.KeyFile != "" {
			clientCertPath = restConfig.CertFile
			clientKeyPath = restConfig.KeyFile
		}
		// 如果配置中有证书数据，写入临时文件
		if restConfig.CertData != nil && restConfig.KeyData != nil {
			clientCertPath = "/tmp/kubelet-client.crt"
			clientKeyPath = "/tmp/kubelet-client.key"
			if err := os.WriteFile(clientCertPath, restConfig.CertData, 0600); err != nil {
				return nil, fmt.Errorf("failed to write client cert: %v", err)
			}
			if err := os.WriteFile(clientKeyPath, restConfig.KeyData, 0600); err != nil {
				return nil, fmt.Errorf("failed to write client key: %v", err)
			}
		}

		// 从 kubeconfig 中提取 CA 证书信息
		if restConfig.CAData != nil {
			caPath = "/tmp/kubelet-ca.crt"
			if err := os.WriteFile(caPath, restConfig.CAData, 0644); err != nil {
				return nil, fmt.Errorf("failed to write CA cert: %v", err)
			}
		} else if restConfig.CAFile != "" {
			caPath = restConfig.CAFile
		}
	} else {
		// 使用 kubelet API 模式，证书路径为空（使用 token 认证）
		clientCertPath = ""
		clientKeyPath = ""
	}

	return &KubeClient{
		Clientset:         clientset,
		NodeName:          nodeName,
		KubeletHost:       kubeletHost,
		KubeletPort:       kubeletPort,
		KubeletSkipVerify: cfg.KubeletSkipVerify,
		KubeletCAPath:     caPath,
		KubeletClientCert: clientCertPath,
		KubeletClientKey:  clientKeyPath,
		KubeletTokenPath:  cfg.KubeletTokenPath,
		KubeletServerName: cfg.KubeletServerName,
		SATokenPath:       saTokenPath,
		RestConfig:        restConfig,
		cadvisorCalc:      cadvisor.NewCalculator(),
	}, nil
}

// NewKubeClient 创建KubeClient，nodeName必须由参数传入（兼容性函数）
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
	serverName := os.Getenv("KUBELET_SERVER_NAME")

	// 从 kubeconfig 中提取客户端证书信息（如果环境变量未设置）
	if clientCert == "" && clientKey == "" {
		if config.CertFile != "" && config.KeyFile != "" {
			clientCert = config.CertFile
			clientKey = config.KeyFile
		}
		// 如果配置中有证书数据，写入临时文件
		if config.CertData != nil && config.KeyData != nil {
			clientCert = "/tmp/kubelet-client.crt"
			clientKey = "/tmp/kubelet-client.key"
			if err := os.WriteFile(clientCert, config.CertData, 0600); err != nil {
				return nil, fmt.Errorf("failed to write client cert: %v", err)
			}
			if err := os.WriteFile(clientKey, config.KeyData, 0600); err != nil {
				return nil, fmt.Errorf("failed to write client key: %v", err)
			}
		}
	}

	// 从 kubeconfig 中提取 CA 证书信息（如果环境变量未设置）
	if caPath == "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt" {
		if config.CAData != nil {
			caPath = "/tmp/kubelet-ca.crt"
			if err := os.WriteFile(caPath, config.CAData, 0644); err != nil {
				return nil, fmt.Errorf("failed to write CA cert: %v", err)
			}
		} else if config.CAFile != "" {
			caPath = config.CAFile
		}
	}

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
		KubeletServerName: serverName,
		SATokenPath:       saTokenPath,
		RestConfig:        config,
		cadvisorCalc:      cadvisor.NewCalculator(),
	}, nil
}

// ListNodePods 获取本节点所有Pod（API Server）
func (k *KubeClient) ListNodePods() ([]corev1.Pod, error) {
	if k.Clientset == nil {
		return nil, fmt.Errorf("kubernetes clientset is nil, cannot list pods from API server")
	}
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
	// 如果 Kubernetes 客户端为 nil（kubelet API 模式），不回退到 API Server
	if k.Clientset == nil {
		return nil, fmt.Errorf("failed to get pods from kubelet and kubernetes clientset is nil: %v", err)
	}
	return k.ListNodePods()
}

// WatchNodePods 监听本节点Pod事件
func (k *KubeClient) WatchNodePods() (watch.Interface, error) {
	if k.Clientset == nil {
		return nil, fmt.Errorf("kubernetes clientset is nil, cannot watch pods")
	}
	fieldSelector := fields.OneTermEqualSelector("spec.nodeName", k.NodeName).String()
	return k.Clientset.CoreV1().Pods("").Watch(context.TODO(), metav1.ListOptions{
		FieldSelector: fieldSelector,
		Watch:         true,
	})
}

// GetPod 获取指定命名空间和名称的Pod
func (k *KubeClient) GetPod(namespace, name string) (*corev1.Pod, error) {
	if k.Clientset == nil {
		return nil, fmt.Errorf("kubernetes clientset is nil, cannot get pod")
	}
	pod, err := k.Clientset.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %v", err)
	}
	return pod, nil
}

// UpdatePod 更新指定Pod
func (k *KubeClient) UpdatePod(pod *corev1.Pod) (*corev1.Pod, error) {
	if k.Clientset == nil {
		return nil, fmt.Errorf("kubernetes clientset is nil, cannot update pod")
	}
	pod, err := k.Clientset.CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update pod: %v", err)
	}
	return pod, nil
}

// CreateEvent 在指定 Pod 上创建事件
func (k *KubeClient) CreateEvent(namespace, podName, eventType, reason, message string) error {
	if k.Clientset == nil {
		return fmt.Errorf("kubernetes clientset is nil, cannot create event")
	}
	ref, err := k.Clientset.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: podName + "-smartlimit-",
			Namespace:    namespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:       "Pod",
			Namespace:  namespace,
			Name:       podName,
			UID:        ref.UID,
			APIVersion: "v1",
		},
		Reason:  reason,
		Message: message,
		Source: corev1.EventSource{
			Component: "smartlimit-controller",
		},
		FirstTimestamp: metav1.Now(),
		LastTimestamp:  metav1.Now(),
		Type:           eventType,
	}
	_, err = k.Clientset.CoreV1().Events(namespace).Create(context.TODO(), event, metav1.CreateOptions{})
	return err
}
