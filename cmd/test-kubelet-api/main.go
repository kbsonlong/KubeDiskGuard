package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"KubeDiskGuard/pkg/kubelet"
)

func main() {
	var (
		host       = flag.String("host", "localhost", "kubelet host")
		port       = flag.String("port", "10250", "kubelet port")
		tokenPath  = flag.String("token", "", "kubelet token path")
		caPath     = flag.String("ca", "", "kubelet CA certificate path")
		skipVerify = flag.Bool("skip-verify", true, "skip TLS verification")
	)
	flag.Parse()

	// 创建kubelet客户端
	client, err := kubelet.NewKubeletClient(*host, *port, *tokenPath, *caPath, *skipVerify)
	if err != nil {
		log.Fatalf("Failed to create kubelet client: %v", err)
	}

	fmt.Printf("Testing kubelet API at %s:%s\n", *host, *port)
	fmt.Println(strings.Repeat("=", 50))

	// 1. 测试健康检查
	fmt.Println("1. Testing health check...")
	if err := testHealthCheck(client); err != nil {
		fmt.Printf("Health check failed: %v\n", err)
	} else {
		fmt.Println("✓ Health check passed")
	}

	// 2. 测试节点摘要API
	fmt.Println("\n2. Testing node summary API...")
	if err := testNodeSummary(client); err != nil {
		fmt.Printf("Node summary failed: %v\n", err)
	} else {
		fmt.Println("✓ Node summary passed")
	}

	// 3. 测试cAdvisor指标
	fmt.Println("\n3. Testing cAdvisor metrics...")
	if err := testCadvisorMetrics(client); err != nil {
		fmt.Printf("cAdvisor metrics failed: %v\n", err)
	} else {
		fmt.Println("✓ cAdvisor metrics passed")
	}

	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("Test completed")
}

func testHealthCheck(client *kubelet.KubeletClient) error {
	// 这里可以添加健康检查逻辑
	// 由于我们没有直接的健康检查方法，我们通过获取节点摘要来间接测试
	_, err := client.GetNodeSummary()
	return err
}

func testNodeSummary(client *kubelet.KubeletClient) error {
	summary, err := client.GetNodeSummary()
	if err != nil {
		return err
	}

	fmt.Printf("Node: %s\n", summary.Node.Name)
	fmt.Printf("Pods count: %d\n", len(summary.Pods))

	// 检查是否有IO统计信息
	hasIOStats := false
	for _, pod := range summary.Pods {
		for _, container := range pod.Containers {
			if container.DiskIO != nil {
				hasIOStats = true
				fmt.Printf("Found IO stats for container %s in pod %s/%s\n",
					container.Name, pod.PodRef.Namespace, pod.PodRef.Name)
				fmt.Printf("  Read IOPS: %d, Write IOPS: %d\n",
					container.DiskIO.ReadIOPS, container.DiskIO.WriteIOPS)
				fmt.Printf("  Read Bytes: %d, Write Bytes: %d\n",
					container.DiskIO.ReadBytes, container.DiskIO.WriteBytes)
			}
		}
	}

	if !hasIOStats {
		fmt.Println("⚠ No IO statistics found in node summary")
	}

	return nil
}

func testCadvisorMetrics(client *kubelet.KubeletClient) error {
	metrics, err := client.GetCadvisorMetrics()
	if err != nil {
		return err
	}

	fmt.Printf("cAdvisor metrics length: %d characters\n", len(metrics))

	// 解析指标
	parsedMetrics, err := client.ParseCadvisorMetrics(metrics)
	if err != nil {
		return err
	}

	fmt.Printf("Parsed metrics:\n")
	fmt.Printf("  Container FS capacity: %d entries\n", len(parsedMetrics.ContainerFSCapacityBytes))
	fmt.Printf("  Container FS usage: %d entries\n", len(parsedMetrics.ContainerFSUsageBytes))
	fmt.Printf("  Container FS reads total: %d entries\n", len(parsedMetrics.ContainerFSReadsTotal))
	fmt.Printf("  Container FS writes total: %d entries\n", len(parsedMetrics.ContainerFSWritesTotal))
	fmt.Printf("  Container FS reads bytes: %d entries\n", len(parsedMetrics.ContainerFSReadsBytesTotal))
	fmt.Printf("  Container FS writes bytes: %d entries\n", len(parsedMetrics.ContainerFSWritesBytesTotal))

	// 显示前几个容器的IO统计
	count := 0
	for containerID, readIOPS := range parsedMetrics.ContainerFSReadsTotal {
		if count >= 3 {
			break
		}
		writeIOPS := parsedMetrics.ContainerFSWritesTotal[containerID]
		readBytes := parsedMetrics.ContainerFSReadsBytesTotal[containerID]
		writeBytes := parsedMetrics.ContainerFSWritesBytesTotal[containerID]

		fmt.Printf("  Container %s: Read IOPS=%.0f, Write IOPS=%.0f, Read Bytes=%.0f, Write Bytes=%.0f\n",
			containerID, readIOPS, writeIOPS, readBytes, writeBytes)
		count++
	}

	return nil
}
