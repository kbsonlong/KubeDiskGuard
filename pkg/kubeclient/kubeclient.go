package kubeclient

import (
	"context"
	"crypto/tls"
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
type KubeClient struct {
	Clientset         *kubernetes.Clientset
	NodeName          string
	KubeletHost       string
	KubeletPort       string
	KubeletSkipVerify bool
	SATokenPath       string
}

// NewKubeClient 创建KubeClient，优先in-cluster，失败则用kubeconfig
func NewKubeClient(nodeName, kubeconfigPath string) (*KubeClient, error) {
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

	if nodeName == "" {
		nodeName, _ = os.Hostname()
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

	return &KubeClient{
		Clientset:         clientset,
		NodeName:          nodeName,
		KubeletHost:       kubeletHost,
		KubeletPort:       kubeletPort,
		KubeletSkipVerify: kubeletSkipVerify,
		SATokenPath:       saTokenPath,
	}, nil
}

// GetNodePodsFromKubelet 使用kubelet API获取本节点Pod信息，支持ServiceAccount Token认证
func (k *KubeClient) GetNodePodsFromKubelet() ([]corev1.Pod, error) {
	kubeletURL := fmt.Sprintf("https://%s:%s/pods", k.KubeletHost, k.KubeletPort)

	token, err := ioutil.ReadFile(k.SATokenPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read serviceaccount token: %v", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: k.KubeletSkipVerify,
			},
		},
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", kubeletURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(token))

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
