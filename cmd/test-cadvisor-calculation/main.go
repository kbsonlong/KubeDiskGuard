package main

import (
	"flag"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"KubeDiskGuard/pkg/cadvisor"
)

func main() {
	var (
		containerID = flag.String("container", "test-container", "容器ID")
		duration    = flag.Duration("duration", 5*time.Minute, "模拟数据持续时间")
		interval    = flag.Duration("interval", 10*time.Second, "数据采集间隔")
	)
	flag.Parse()

	fmt.Printf("开始模拟 cAdvisor 指标计算测试\n")
	fmt.Printf("容器ID: %s\n", *containerID)
	fmt.Printf("持续时间: %v\n", *duration)
	fmt.Printf("采集间隔: %v\n", *interval)
	fmt.Println(strings.Repeat("=", 60))

	// 创建计算器
	calculator := cadvisor.NewCalculator()

	// 模拟数据生成
	startTime := time.Now()
	endTime := startTime.Add(*duration)
	currentTime := startTime

	// 初始累积值
	var readIOPS, writeIOPS, readBytes, writeBytes float64

	fmt.Println("时间点\t\t累积读取IOPS\t累积写入IOPS\t累积读取字节\t累积写入字节")
	fmt.Println(strings.Repeat("-", 100))

	for currentTime.Before(endTime) {
		// 模拟IO活动（随机增长）
		readIOPS += float64(rand.Intn(50) + 10)                // 每次增加10-60
		writeIOPS += float64(rand.Intn(30) + 5)                // 每次增加5-35
		readBytes += float64(rand.Intn(1024*1024) + 1024*1024) // 每次增加1-2MB
		writeBytes += float64(rand.Intn(512*1024) + 512*1024)  // 每次增加0.5-1MB

		// 添加数据点
		calculator.AddMetricPoint(*containerID, currentTime, readIOPS, writeIOPS, readBytes, writeBytes)

		// 打印当前累积值
		fmt.Printf("%s\t%.0f\t\t%.0f\t\t%.0f\t\t%.0f\n",
			currentTime.Format("15:04:05"),
			readIOPS, writeIOPS, readBytes, writeBytes)

		currentTime = currentTime.Add(*interval)
		time.Sleep(100 * time.Millisecond) // 模拟时间流逝
	}

	fmt.Println(strings.Repeat("-", 100))

	// 计算不同时间窗口的IO速率
	windows := []time.Duration{
		30 * time.Second,
		1 * time.Minute,
		2 * time.Minute,
		5 * time.Minute,
	}

	fmt.Println("\nIO速率计算结果:")
	fmt.Println(strings.Repeat("=", 60))

	for _, window := range windows {
		rate, err := calculator.CalculateIORate(*containerID, window)
		if err != nil {
			fmt.Printf("窗口 %v: 计算失败 - %v\n", window, err)
			continue
		}

		fmt.Printf("时间窗口: %v\n", window)
		fmt.Printf("  读取 IOPS: %.2f ops/s\n", rate.ReadIOPS)
		fmt.Printf("  写入 IOPS: %.2f ops/s\n", rate.WriteIOPS)
		fmt.Printf("  读取 BPS:  %.2f bytes/s (%.2f MB/s)\n", rate.ReadBPS, rate.ReadBPS/1024/1024)
		fmt.Printf("  写入 BPS:  %.2f bytes/s (%.2f MB/s)\n", rate.WriteBPS, rate.WriteBPS/1024/1024)
		fmt.Println()
	}

	// 计算平均IO速率
	fmt.Println("平均IO速率 (多窗口平均):")
	fmt.Println(strings.Repeat("-", 40))
	avgRate, err := calculator.CalculateAverageIORate(*containerID, windows)
	if err != nil {
		fmt.Printf("计算平均速率失败: %v\n", err)
	} else {
		fmt.Printf("平均读取 IOPS: %.2f ops/s\n", avgRate.ReadIOPS)
		fmt.Printf("平均写入 IOPS: %.2f ops/s\n", avgRate.WriteIOPS)
		fmt.Printf("平均读取 BPS:  %.2f bytes/s (%.2f MB/s)\n", avgRate.ReadBPS, avgRate.ReadBPS/1024/1024)
		fmt.Printf("平均写入 BPS:  %.2f bytes/s (%.2f MB/s)\n", avgRate.WriteBPS, avgRate.WriteBPS/1024/1024)
	}

	// 显示统计信息
	fmt.Println("\n统计信息:")
	fmt.Println(strings.Repeat("-", 20))
	containerCount := calculator.GetContainerCount()
	dataPointCount := calculator.GetTotalDataPoints()
	fmt.Printf("容器数量: %d\n", containerCount)
	fmt.Printf("数据点总数: %d\n", dataPointCount)

	// 显示历史数据
	fmt.Println("\n历史数据点 (最近5个):")
	fmt.Println(strings.Repeat("-", 30))
	history := calculator.GetContainerHistory(*containerID)
	if len(history) > 0 {
		start := len(history) - 5
		if start < 0 {
			start = 0
		}
		for i := start; i < len(history); i++ {
			point := history[i]
			fmt.Printf("%s: 读取IOPS=%.0f, 写入IOPS=%.0f, 读取字节=%.0f, 写入字节=%.0f\n",
				point.Timestamp.Format("15:04:05"),
				point.ReadIOPS, point.WriteIOPS, point.ReadBytes, point.WriteBytes)
		}
	}

	fmt.Println("\n测试完成!")
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
