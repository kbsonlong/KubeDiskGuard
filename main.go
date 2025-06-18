package main

import (
	"log"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/service"
)

func main() {
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

	// 运行服务
	if err := svc.Run(); err != nil {
		log.Fatalf("Service failed: %v", err)
	}
}
