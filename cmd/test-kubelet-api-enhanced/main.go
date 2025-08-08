package main

import (
	"fmt"
	"os"
	"strings"

	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/kubeclient"
)

func main() {
	fmt.Println("=== Enhanced KubeClient kubelet API 测试 ===")

	// 1. 获取节点名称
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		nodeName = "localhost" // 默认值，用于测试
		fmt.Printf("Warning: NODE_NAME not set, using default: %s\n", nodeName)
	}

	// 2. 获取 kubeconfig 路径
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("HOME") + "/.kube/config"
	}
	fmt.Printf("Using kubeconfig: %s\n", kubeconfigPath)

	// 3. 创建配置
	cfg := &config.Config{
		KubeletHost:       os.Getenv("KUBELET_HOST"),       // 默认 localhost
		KubeletPort:       os.Getenv("KUBELET_PORT"),       // 默认 10250
		KubeletSkipVerify: os.Getenv("KUBELET_SKIP_VERIFY") == "true",
		KubeletCAPath:     os.Getenv("KUBELET_CA_PATH"),
		KubeletTokenPath:  os.Getenv("KUBELET_TOKEN_PATH"),
		KubeletServerName: os.Getenv("KUBELET_SERVER_NAME"),
	}

	// 4. 创建 KubeClient
	client, err := kubeclient.NewKubeClientWithConfig(nodeName, kubeconfigPath, cfg)
	if err != nil {
		fmt.Printf("创建 KubeClient 失败: %v\n", err)
		return
	}

	fmt.Printf("✓ 成功创建 KubeClient\n")
	fmt.Printf("  - Node Name: %s\n", client.NodeName)
	fmt.Printf("  - Kubelet Host: %s\n", client.KubeletHost)
	fmt.Printf("  - Kubelet Port: %s\n", client.KubeletPort)
	fmt.Printf("  - Skip Verify: %v\n", client.KubeletSkipVerify)
	fmt.Printf("  - Server Name: %s\n", client.KubeletServerName)

	// 显示认证方式
	if client.KubeletClientCert != "" && client.KubeletClientKey != "" {
		fmt.Printf("  - 认证方式: 客户端证书\n")
		fmt.Printf("  - Client Cert: %s\n", client.KubeletClientCert)
		fmt.Printf("  - Client Key: %s\n", client.KubeletClientKey)
	} else if client.KubeletTokenPath != "" || client.SATokenPath != "" {
		fmt.Printf("  - 认证方式: ServiceAccount Token\n")
		if client.KubeletTokenPath != "" {
			fmt.Printf("  - Token Path: %s\n", client.KubeletTokenPath)
		} else {
			fmt.Printf("  - SA Token Path: %s\n", client.SATokenPath)
		}
	} else {
		fmt.Printf("  - 认证方式: 无认证\n")
	}
	fmt.Printf("  - CA Path: %s\n", client.KubeletCAPath)

	// 5. 测试 kubelet 连接
	fmt.Println("\n=== 测试 kubelet 连接 ===")
	if err := client.TestKubeletConnection(); err != nil {
		fmt.Printf("连接测试失败: %v\n", err)
		return
	}

	// 6. 获取节点摘要
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

	// 7. 获取 cAdvisor 指标（可选）
	fmt.Println("\n=== 获取 cAdvisor 指标 ===")
	metrics, err := client.GetCadvisorMetrics()
	if err != nil {
		fmt.Printf("获取 cAdvisor 指标失败: %v\n", err)
	} else {
		fmt.Printf("✓ 成功获取 cAdvisor 指标 (长度: %d 字符)\n", len(metrics))
		// 显示前几行
		lines := strings.Split(metrics, "\n")
		for i, line := range lines {
			if i >= 5 { // 只显示前5行
				break
			}
			if strings.TrimSpace(line) != "" {
				fmt.Printf("  %s\n", line)
			}
		}
	}

	// 8. 测试从 kubelet 获取 Pod 列表
	fmt.Println("\n=== 测试从 kubelet 获取 Pod 列表 ===")
	pods, err := client.ListNodePodsWithKubeletFirst()
	if err != nil {
		fmt.Printf("获取 Pod 列表失败: %v\n", err)
	} else {
		fmt.Printf("✓ 成功获取 Pod 列表 (数量: %d)\n", len(pods))
		for i, pod := range pods {
			if i >= 3 { // 只显示前3个
				break
			}
			fmt.Printf("  - Pod[%d]: %s/%s (状态: %s)\n",
				i, pod.Namespace, pod.Name, pod.Status.Phase)
		}
	}

	fmt.Println("\n=== 测试完成 ===")
}