package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubeletClient 封装 kubelet API 访问
type KubeletClient struct {
	KubernetesClient *kubernetes.Clientset
	KubeletHost      string
	KubeletPort      string
	TokenPath        string
	CAPath           string
	ClientCertPath   string
	ClientKeyPath    string
	SkipVerify       bool
	ServerName       string
	Config           *rest.Config // 保存 kubeconfig 配置
}

// NodeSummary kubelet API 返回的节点摘要
type NodeSummary struct {
	Node NodeStats  `json:"node"`
	Pods []PodStats `json:"pods"`
}

type NodeStats struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
}

type PodStats struct {
	PodRef     PodReference     `json:"podRef"`
	Timestamp  time.Time        `json:"timestamp"`
	Containers []ContainerStats `json:"containers"`
}

type PodReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
}

type ContainerStats struct {
	Name      string       `json:"name"`
	Timestamp time.Time    `json:"timestamp"`
	DiskIO    *DiskIOStats `json:"diskio,omitempty"`
}

type DiskIOStats struct {
	ReadBytes  uint64 `json:"readBytes"`
	WriteBytes uint64 `json:"writeBytes"`
	ReadIOPS   uint64 `json:"readIOPS"`
	WriteIOPS  uint64 `json:"writeIOPS"`
}

// NewKubeletClientFromKubeconfig 从 kubeconfig 创建 kubelet 客户端
func NewKubeletClientFromKubeconfig(kubeconfigPath, kubeletHost, kubeletPort string) (*KubeletClient, error) {
	// 1. 加载 kubeconfig
	var config *rest.Config
	var err error

	if kubeconfigPath != "" {
		// 从指定路径加载 kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load kubeconfig from %s: %v", kubeconfigPath, err)
		}
	} else {
		// 尝试从环境变量 KUBECONFIG 加载
		if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
			config, err = clientcmd.BuildConfigFromFlags("", envPath)
			if err != nil {
				return nil, fmt.Errorf("failed to load kubeconfig from env: %v", err)
			}
		} else {
			// 尝试使用 in-cluster config
			config, err = rest.InClusterConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
			}
		}
	}

	// 2. 创建 Kubernetes 客户端
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %v", err)
	}

	// 3. 设置 kubelet 连接参数
	if kubeletHost == "" {
		kubeletHost = os.Getenv("KUBELET_HOST")
		if kubeletHost == "" {
			kubeletHost = "localhost"
		}
	}

	if kubeletPort == "" {
		kubeletPort = os.Getenv("KUBELET_PORT")
		if kubeletPort == "" {
			kubeletPort = "10250"
		}
	}

	// 4. 从 kubeconfig 中提取认证信息
	var tokenPath, caPath, clientCertPath, clientKeyPath string

	// 优先使用 kubeconfig 中的客户端证书认证
	if config.CertData != nil && config.KeyData != nil {
		// 将证书数据写入临时文件
		clientCertPath = "/tmp/kubelet-client.crt"
		clientKeyPath = "/tmp/kubelet-client.key"

		if err := os.WriteFile(clientCertPath, config.CertData, 0600); err != nil {
			return nil, fmt.Errorf("failed to write client cert: %v", err)
		}
		if err := os.WriteFile(clientKeyPath, config.KeyData, 0600); err != nil {
			return nil, fmt.Errorf("failed to write client key: %v", err)
		}
	} else if config.CertFile != "" && config.KeyFile != "" {
		// 使用证书文件路径
		clientCertPath = config.CertFile
		clientKeyPath = config.KeyFile
	} else {
		// 回退到 ServiceAccount token
		tokenPath = os.Getenv("KUBELET_SA_TOKEN_PATH")
		if tokenPath == "" {
			tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
		}
	}

	// 5. 设置 CA 证书
	if config.CAData != nil {
		// 将 CA 数据写入临时文件
		caPath = "/tmp/kubelet-ca.crt"
		if err := os.WriteFile(caPath, config.CAData, 0644); err != nil {
			return nil, fmt.Errorf("failed to write CA cert: %v", err)
		}
	} else if config.CAFile != "" {
		// 使用 CA 文件路径
		caPath = config.CAFile
	} else {
		// 回退到默认 ServiceAccount CA
		caPath = os.Getenv("KUBELET_CA_PATH")
		if caPath == "" {
			caPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
		}
	}

	// 6. 其他配置参数
	skipVerify := os.Getenv("KUBELET_SKIP_VERIFY") == "true"
	serverName := os.Getenv("KUBELET_SERVER_NAME")

	return &KubeletClient{
		KubernetesClient: clientset,
		KubeletHost:      kubeletHost,
		KubeletPort:      kubeletPort,
		TokenPath:        tokenPath,
		CAPath:           caPath,
		ClientCertPath:   clientCertPath,
		ClientKeyPath:    clientKeyPath,
		SkipVerify:       skipVerify,
		ServerName:       serverName,
		Config:           config,
	}, nil
}

// createHTTPClient 创建用于访问 kubelet API 的 HTTP 客户端
func (kc *KubeletClient) createHTTPClient() (*http.Client, string, error) {
	// 1. 读取认证 token（如果使用 token 认证）
	var token string
	if kc.TokenPath != "" {
		if tokenBytes, err := os.ReadFile(kc.TokenPath); err == nil {
			token = strings.TrimSpace(string(tokenBytes))
			fmt.Printf("Using ServiceAccount token authentication\n")
		} else {
			fmt.Printf("Warning: failed to read token from %s: %v\n", kc.TokenPath, err)
		}
	}

	// 2. 配置 TLS
	tlsConfig := &tls.Config{
		InsecureSkipVerify: kc.SkipVerify,
	}

	// 设置 ServerName 以匹配证书
	if kc.ServerName != "" {
		tlsConfig.ServerName = kc.ServerName
	} else if kc.KubeletHost == "localhost" {
		// Kind 集群的特殊处理
		tlsConfig.ServerName = "kind-control-plane"
	} else {
		tlsConfig.ServerName = kc.KubeletHost
	}

	// 3. 加载客户端证书（如果使用证书认证）
	if kc.ClientCertPath != "" && kc.ClientKeyPath != "" {
		clientCert, err := tls.LoadX509KeyPair(kc.ClientCertPath, kc.ClientKeyPath)
		if err != nil {
			return nil, "", fmt.Errorf("failed to load client certificate: %v", err)
		}
		tlsConfig.Certificates = []tls.Certificate{clientCert}
		fmt.Printf("Using client certificate authentication\n")
	}

	// 4. 加载 CA 证书
	if kc.CAPath != "" && !kc.SkipVerify {
		if caCert, err := os.ReadFile(kc.CAPath); err == nil {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				tlsConfig.RootCAs = caCertPool
				tlsConfig.InsecureSkipVerify = false
				fmt.Printf("Using CA certificate from: %s\n", kc.CAPath)
			}
		} else {
			fmt.Printf("Warning: failed to read CA cert from %s: %v\n", kc.CAPath, err)
		}
	}

	// 5. 创建 HTTP 客户端
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return client, token, nil
}

// doKubeletRequest 执行 kubelet API 请求
func (kc *KubeletClient) doKubeletRequest(path string) ([]byte, error) {
	// 1. 创建 HTTP 客户端和获取 token
	client, token, err := kc.createHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %v", err)
	}

	// 2. 构建请求 URL
	url := fmt.Sprintf("https://%s:%s%s", kc.KubeletHost, kc.KubeletPort, path)

	// 3. 创建 HTTP 请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// 4. 设置认证头
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		fmt.Printf("Request using Bearer token authentication\n")
	} else if kc.ClientCertPath != "" && kc.ClientKeyPath != "" {
		fmt.Printf("Request using client certificate authentication\n")
	} else {
		fmt.Printf("Warning: No authentication method configured\n")
	}
	req.Header.Set("Accept", "application/json")

	// 5. 执行请求
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to perform request to %s: %v", path, err)
	}
	defer resp.Body.Close()

	// 6. 检查响应状态
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("request to %s failed with status %d: %s", path, resp.StatusCode, string(body))
	}

	// 7. 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return body, nil
}

// GetNodeSummary 获取节点摘要信息
func (kc *KubeletClient) GetNodeSummary() (*NodeSummary, error) {
	body, err := kc.doKubeletRequest("/stats/summary")
	if err != nil {
		return nil, fmt.Errorf("failed to get node summary: %v", err)
	}

	var summary NodeSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return nil, fmt.Errorf("failed to parse node summary: %v", err)
	}

	return &summary, nil
}

// GetCadvisorMetrics 获取 cAdvisor 指标
func (kc *KubeletClient) GetCadvisorMetrics() (string, error) {
	body, err := kc.doKubeletRequest("/metrics/cadvisor")
	if err != nil {
		return "", fmt.Errorf("failed to get cadvisor metrics: %v", err)
	}

	return string(body), nil
}

// TestKubeletConnection 测试 kubelet 连接
func (kc *KubeletClient) TestKubeletConnection() error {
	_, err := kc.doKubeletRequest("/healthz")
	if err != nil {
		return fmt.Errorf("kubelet health check failed: %v", err)
	}

	fmt.Println("✓ kubelet 连接测试成功")
	return nil
}

func main() {
	// 示例用法
	fmt.Println("=== client-go 访问 kubelet API 示例 ===")

	// 1. 从 kubeconfig 创建客户端
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("HOME") + "/.kube/config"
	}

	client, err := NewKubeletClientFromKubeconfig(kubeconfigPath, "localhost", "10250")
	if err != nil {
		fmt.Printf("创建客户端失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 成功创建 kubelet 客户端\n")
	fmt.Printf("  - Kubelet Host: %s\n", client.KubeletHost)
	fmt.Printf("  - Kubelet Port: %s\n", client.KubeletPort)
	fmt.Printf("  - Skip Verify: %v\n", client.SkipVerify)
	fmt.Printf("  - Server Name: %s\n", client.ServerName)

	// 显示认证方式
	if client.ClientCertPath != "" && client.ClientKeyPath != "" {
		fmt.Printf("  - 认证方式: 客户端证书\n")
		fmt.Printf("  - Client Cert: %s\n", client.ClientCertPath)
		fmt.Printf("  - Client Key: %s\n", client.ClientKeyPath)
	} else if client.TokenPath != "" {
		fmt.Printf("  - 认证方式: ServiceAccount Token\n")
		fmt.Printf("  - Token Path: %s\n", client.TokenPath)
	} else {
		fmt.Printf("  - 认证方式: 无认证\n")
	}
	fmt.Printf("  - CA Path: %s\n", client.CAPath)

	// 2. 测试连接
	fmt.Println("\n=== 测试 kubelet 连接 ===")
	if err := client.TestKubeletConnection(); err != nil {
		fmt.Printf("连接测试失败: %v\n", err)
		return
	}

	// 3. 获取节点摘要
	fmt.Println("\n=== 获取节点摘要 ===")
	summary, err := client.GetNodeSummary()
	if err != nil {
		fmt.Printf("获取节点摘要失败: %v\n", err)
	} else {
		fmt.Printf("✓ 成功获取节点摘要\n")
		fmt.Printf("  - 节点名称: %s\n", summary.Node.Name)
		fmt.Printf("  - Pod 数量: %d\n", len(summary.Pods))

		// 显示前几个 Pod 的信息
		for i, pod := range summary.Pods {
			if i >= 3 { // 只显示前3个
				break
			}
			fmt.Printf("  - Pod[%d]: %s/%s (容器数: %d)\n",
				i, pod.PodRef.Namespace, pod.PodRef.Name, len(pod.Containers))
		}
	}

	// 4. 获取 cAdvisor 指标（可选）
	fmt.Println("\n=== 获取 cAdvisor 指标 ===")
	metrics, err := client.GetCadvisorMetrics()
	if err != nil {
		fmt.Printf("获取 cAdvisor 指标失败: %v\n", err)
	} else {
		fmt.Printf("✓ 成功获取 cAdvisor 指标 (长度: %d 字符)\n", len(metrics))
		// 显示前几行
		lines := strings.Split(metrics, "\n")
		for i, line := range lines {
			if i >= 50 { // 只显示前5行
				break
			}
			if strings.TrimSpace(line) != "" {
				fmt.Printf("  %s\n", line)
			}
		}
	}

	fmt.Println("\n=== 示例完成 ===")
}
