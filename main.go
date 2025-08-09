package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"KubeDiskGuard/pkg/api"
	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/service"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	metricsAddr := flag.String("metrics-addr", ":2112", "Prometheus metrics监听地址")
	flag.Parse()

	if *version {
		log.Printf("KubeDiskGuard 版本信息: version=%s, build_time=%s", Version, BuildTime)
		os.Exit(0)
	}

	// 创建路由器
	router := mux.NewRouter()

	// 添加 Prometheus metrics 和健康检查
	router.Handle("/metrics", promhttp.Handler())
	router.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// 启动HTTP服务器
	go func() {
		log.Printf("[INFO] HTTP server listening on %s", *metricsAddr)
		if err := http.ListenAndServe(*metricsAddr, router); err != nil {
			log.Fatalf("[FATAL] HTTP server error: %v", err)
		}
	}()

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

	// 创建并注册 API 服务器
	apiServer := api.NewAPIServer(svc.GetSmartLimitManager())
	apiServer.RegisterRoutes(router)
	log.Printf("[INFO] API routes registered")

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
