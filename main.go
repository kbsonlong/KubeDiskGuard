package main

import (
	"flag"
	"log"
	"os"

	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/service"
)

var (
	Version   = "dev"
	GitCommit = "dev"
	BuildTime = "unknown"
)

func main() {
	// 命令行参数
	resetAll := flag.Bool("reset-all", false, "解除所有容器的IOPS限速")
	version := flag.Bool("version", false, "显示版本信息")
	flag.Parse()

	if *version {
		log.Printf("KubeDiskGuard 版本信息: version=%s, build_time=%s", Version, BuildTime)
		os.Exit(0)
	}

	// 获取默认配置
	cfg := config.GetDefaultConfig()

	// 从环境变量加载配置
	config.LoadFromEnv(cfg)

	// 打印配置
	log.Printf("Configuration: %s", cfg.ToJSON())

	// 创建并运行服务
	svc, err := service.NewKubeDiskGuardService(cfg)
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

	// 运行服务
	if err := svc.Run(); err != nil {
		log.Fatalf("Service failed: %v", err)
	}
}
