package main

import (
	"flag"
	"log"
	"os"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/service"
)

func main() {
	// 命令行参数
	resetAll := flag.Bool("reset-all", false, "解除所有容器的IOPS限速")
	resetOne := flag.String("reset-one", "", "解除指定容器ID的IOPS限速")
	flag.Parse()

	// 获取默认配置
	cfg := config.GetDefaultConfig()

	// 从环境变量加载配置
	config.LoadFromEnv(cfg)

	// 打印配置
	log.Printf("Configuration: %s", cfg.ToJSON())

	// 创建并运行服务
	svc, err := service.NewIOPSLimitService(cfg)
	if err != nil {
		log.Fatalf("Failed to create IOPS limit service: %v", err)
	}

	if *resetAll {
		if err := svc.ResetAllContainersIOPSLimit(); err != nil {
			log.Fatalf("Failed to reset all containers IOPS limit: %v", err)
		}
		log.Println("已解除所有容器的IOPS限速")
		os.Exit(0)
	}
	if *resetOne != "" {
		if err := svc.ResetOneContainerIOPSLimit(*resetOne); err != nil {
			log.Fatalf("Failed to reset container %s IOPS limit: %v", *resetOne, err)
		}
		log.Printf("已解除容器 %s 的IOPS限速", *resetOne)
		os.Exit(0)
	}

	// 运行服务
	if err := svc.Run(); err != nil {
		log.Fatalf("Service failed: %v", err)
	}
}
