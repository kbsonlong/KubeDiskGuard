package kubeclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

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
}

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
	}, nil
}

// GetNodePodsFromKubelet 使用kubelet API获取本节点Pod信息，支持ServiceAccount Token认证和自定义证书
func (k *KubeClient) GetNodePodsFromKubelet() ([]corev1.Pod, error) {
	kubeletURL := fmt.Sprintf("https://%s:%s/pods", k.KubeletHost, k.KubeletPort)

	// 读取Token（可选）
	token := ""
	tokenPath := k.KubeletTokenPath
	if tokenPath == "" {
		tokenPath = k.SATokenPath
	}
	if tokenPath != "" {
		b, err := ioutil.ReadFile(tokenPath)
		if err == nil {
			token = string(b)
		}
	}

	// 构造TLS配置
	tlsConfig := &tls.Config{
		InsecureSkipVerify: k.KubeletSkipVerify,
	}
	if k.KubeletCAPath != "" {
		caCert, err := ioutil.ReadFile(k.KubeletCAPath)
		if err == nil {
			caPool := x509.NewCertPool()
			caPool.AppendCertsFromPEM(caCert)
			tlsConfig.RootCAs = caPool
		}
	}
	if k.KubeletClientCert != "" && k.KubeletClientKey != "" {
		cert, err := tls.LoadX509KeyPair(k.KubeletClientCert, k.KubeletClientKey)
		if err == nil {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", kubeletURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request kubelet API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kubelet API returned status: %d", resp.StatusCode)
	}

	var podList corev1.PodList
	if err := json.NewDecoder(resp.Body).Decode(&podList); err != nil {
		return nil, fmt.Errorf("failed to decode kubelet response: %v", err)
	}

	return podList.Items, nil
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
