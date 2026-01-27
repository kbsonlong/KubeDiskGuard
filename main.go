package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"KubeDiskGuard/pkg/config"
	"KubeDiskGuard/pkg/profiler"
	"KubeDiskGuard/pkg/service"
	"KubeDiskGuard/pkg/store"

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

	// 获取默认配置
	cfg := config.GetDefaultConfig()

	// 从环境变量加载配置
	config.LoadFromEnv(cfg)

	// 打印配置
	log.Printf("Configuration: %s", cfg.ToJSON())

	// 启动阶段磁盘性能采集与持久化
	if cfg.ProfilerEnabled {
		p := profiler.NewManager(profiler.Config{
			Enabled:        cfg.ProfilerEnabled,
			TTL:            time.Duration(cfg.ProfilerTTLSeconds) * time.Second,
			StorePath:      cfg.ProfilerStorePath,
			MaxLoadAvg:     cfg.ProfilerMaxLoadAvg,
			MaxSampleSecs:  cfg.ProfilerSampleDuration,
			MaxFileMB:      cfg.ProfilerMaxFileMB,
			MountPoint:     cfg.ProfilerMountPoint,
		})
		st := store.NewJSONStore(cfg.ProfilerStorePath, time.Duration(cfg.ProfilerTTLSeconds)*time.Second)
		if baseline, fresh, err := st.Load(); err == nil && fresh {
			log.Printf("[INFO] Profiler baseline loaded from %s", cfg.ProfilerStorePath)
			applyBaselineToConfig(cfg, baseline)
		} else {
			if err != nil {
				log.Printf("[WARN] Load baseline error: %v", err)
			}
			start := time.Now()
			profiler.MarkRunStart()
			if b, err := p.Sample(); err == nil {
				if err := st.Save(b); err != nil {
					log.Printf("[WARN] Save baseline failed: %v", err)
				}
				applyBaselineToConfig(cfg, b)
				profiler.MarkDuration(time.Since(start).Seconds())
				log.Printf("[INFO] Profiler sampled and applied baseline")
			} else {
				profiler.MarkSkip()
				log.Printf("[WARN] Profiler sampling skipped/failed: %v", err)
			}
		}
	}

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

func applyBaselineToConfig(cfg *config.Config, b profiler.Baseline) {
	for _, prof := range b.Profiles {
		ri := int(float64(prof.RandReadIOPS) * 0.7)
		wi := int(float64(prof.RandWriteIOPS) * 0.7)
		rb := int(float64(prof.SeqReadBPS) * 0.7)
		wb := int(float64(prof.SeqWriteBPS) * 0.7)
		if ri > 0 {
			if cfg.ContainerReadIOPSLimit == 0 || ri < cfg.ContainerReadIOPSLimit {
				cfg.ContainerReadIOPSLimit = ri
			}
		}
		if wi > 0 {
			if cfg.ContainerWriteIOPSLimit == 0 || wi < cfg.ContainerWriteIOPSLimit {
				cfg.ContainerWriteIOPSLimit = wi
			}
		}
		if rb > 0 {
			if cfg.ContainerReadBPSLimit == 0 || rb < cfg.ContainerReadBPSLimit {
				cfg.ContainerReadBPSLimit = rb
			}
		}
		if wb > 0 {
			if cfg.ContainerWriteBPSLimit == 0 || wb < cfg.ContainerWriteBPSLimit {
				cfg.ContainerWriteBPSLimit = wb
			}
		}
		break
	}
}
