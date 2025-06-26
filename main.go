package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus/promhttp"

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
	metricsAddr := flag.String("metrics-addr", ":2112", "Prometheus metrics监听地址")
	flag.Parse()

	if *version {
		log.Printf("KubeDiskGuard 版本信息: version=%s, build_time=%s", Version, BuildTime)
		os.Exit(0)
	}

	// 启动metrics和健康探测接口
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})
		log.Printf("[INFO] Metrics/healthz listening on %s", *metricsAddr)
		if err := http.ListenAndServe(*metricsAddr, nil); err != nil {
			log.Fatalf("[FATAL] Metrics/healthz server error: %v", err)
		}
	}()

	// 加载配置
	cfg := config.GetDefaultConfig()
	config.LoadFromEnv(cfg)

	// 实现配置文件监听，支持动态配置更新
	configPath := os.Getenv("CONFIG_FILE_PATH") // 从环境变量获取配置文件路径
	configWatcher := config.NewConfigWatcher(configPath, cfg)

	// 添加配置更新回调
	configWatcher.AddUpdateCallback(func(newCfg *config.Config) {
		log.Printf("[INFO] 配置已更新，新的IOPS限制: %d", newCfg.ContainerIOPSLimit)
		log.Printf("[INFO] 新的数据挂载点: %s", newCfg.DataMount)
		log.Printf("[INFO] 智能限速是否启用: %t", newCfg.SmartLimitEnabled)
		// 这里可以添加更多的配置更新处理逻辑
		// 例如：通知服务重新初始化某些组件
	})

	// 启动配置文件监听
	if err := configWatcher.Start(); err != nil {
		log.Printf("[WARN] 配置文件监听启动失败: %v，将使用静态配置", err)
	}
	defer configWatcher.Stop()

	// 打印配置
	log.Printf("Configuration: %s", cfg.ToJSON())

	// 创建服务（使用配置监听器获取配置）
	svc, err := service.NewKubeDiskGuardService(configWatcher.GetConfig())
	if err != nil {
		log.Fatalf("创建服务失败: %v", err)
	}

	// 添加服务重新配置的回调
	configWatcher.AddUpdateCallback(func(newCfg *config.Config) {
		// 实现服务的热重载逻辑
		log.Printf("[INFO] 检测到配置更新，开始热重载服务...")

		// 调用服务的热重载方法
		if err := svc.ReloadConfig(newCfg); err != nil {
			log.Printf("[ERROR] 服务热重载失败: %v", err)
		} else {
			log.Printf("[INFO] 服务热重载成功完成")
		}
	})

	// 信号监听
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[INFO] Received signal: %v, shutting down...", sig)
		svc.Stop()
		svc.Wg.Wait() // 等待所有后台任务退出
		os.Exit(0)
	}()

	if *resetAll {
		if err := svc.ResetAllContainersIOPSLimit(); err != nil {
			log.Fatalf("Failed to reset all containers IOPS limit: %v", err)
		}
		log.Println("已解除所有容器的IOPS限速")
		os.Exit(0)
	}

	// 启动主服务（如svc.Run()为阻塞可直接调用，否则用go）
	if err := svc.Run(); err != nil {
		log.Fatalf("Service exited with error: %v", err)
	}
}
