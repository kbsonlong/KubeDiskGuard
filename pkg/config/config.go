package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// Config 配置结构体
type Config struct {
	ContainerIOPSLimit      int      `json:"container_iops_limit"`
	ContainerReadIOPSLimit  int      `json:"container_read_iops_limit"`
	ContainerWriteIOPSLimit int      `json:"container_write_iops_limit"`
	ContainerReadBPSLimit   int      `json:"container_read_bps_limit"`
	ContainerWriteBPSLimit  int      `json:"container_write_bps_limit"`
	DataMount               string   `json:"data_mount"`
	ExcludeKeywords         []string `json:"exclude_keywords"`
	ExcludeNamespaces       []string `json:"exclude_namespaces"`
	ExcludeLabelSelector    string   `json:"exclude_label_selector"`
	ContainerdNamespace     string   `json:"containerd_namespace"`
	ContainerRuntime        string   `json:"container_runtime"`
	CgroupVersion           string   `json:"cgroup_version"`
	ContainerSocketPath     string   `json:"container_socket_path,omitempty"` // 可选字段，默认为空
	KubeletHost             string   `json:"kubelet_host,omitempty"`          // kubelet主机地址
	KubeletPort             string   `json:"kubelet_port,omitempty"`          // kubelet端口
	KubeConfigPath          string   // 支持集群外部运行

	// 智能限速配置
	SmartLimitEnabled          bool    `json:"smart_limit_enabled"`
	SmartLimitMonitorInterval  int     `json:"smart_limit_monitor_interval"`   // 监控间隔（秒）
	SmartLimitHistoryWindow    int     `json:"smart_limit_history_window"`     // 历史数据窗口（分钟）
	SmartLimitHighIOThreshold  float64 `json:"smart_limit_high_io_threshold"`  // 高IO阈值
	SmartLimitHighBPSThreshold float64 `json:"smart_limit_high_bps_threshold"` // 高BPS阈值
	SmartLimitAutoIOPS         int     `json:"smart_limit_auto_iops"`          // 自动限速IOPS值
	SmartLimitAutoBPS          int     `json:"smart_limit_auto_bps"`           // 自动限速BPS值
	SmartLimitAnnotationPrefix string  `json:"smart_limit_annotation_prefix"`  // 注解前缀

	// 分级智能限速配置
	SmartLimitGradedThresholds bool `json:"smart_limit_graded_thresholds"` // 是否启用分级阈值
	// 15分钟窗口阈值和限速值
	SmartLimitIOThreshold15m  float64 `json:"smart_limit_io_threshold_15m"`  // 15分钟IO阈值
	SmartLimitBPSThreshold15m float64 `json:"smart_limit_bps_threshold_15m"` // 15分钟BPS阈值
	SmartLimitIOPSLimit15m    int     `json:"smart_limit_iops_limit_15m"`    // 15分钟限速IOPS值
	SmartLimitBPSLimit15m     int     `json:"smart_limit_bps_limit_15m"`     // 15分钟限速BPS值
	// 30分钟窗口阈值和限速值
	SmartLimitIOThreshold30m  float64 `json:"smart_limit_io_threshold_30m"`  // 30分钟IO阈值
	SmartLimitBPSThreshold30m float64 `json:"smart_limit_bps_threshold_30m"` // 30分钟BPS阈值
	SmartLimitIOPSLimit30m    int     `json:"smart_limit_iops_limit_30m"`    // 30分钟限速IOPS值
	SmartLimitBPSLimit30m     int     `json:"smart_limit_bps_limit_30m"`     // 30分钟限速BPS值
	// 60分钟窗口阈值和限速值
	SmartLimitIOThreshold60m  float64 `json:"smart_limit_io_threshold_60m"`  // 60分钟IO阈值
	SmartLimitBPSThreshold60m float64 `json:"smart_limit_bps_threshold_60m"` // 60分钟BPS阈值
	SmartLimitIOPSLimit60m    int     `json:"smart_limit_iops_limit_60m"`    // 60分钟限速IOPS值
	SmartLimitBPSLimit60m     int     `json:"smart_limit_bps_limit_60m"`     // 60分钟限速BPS值

	// kubelet API配置
KubeletTokenPath        string `json:"kubelet_token_path,omitempty"`  // kubelet token路径
KubeletCAPath           string `json:"kubelet_ca_path,omitempty"`     // kubelet CA证书路径
KubeletServerName       string `json:"kubelet_server_name,omitempty"` // kubelet TLS服务器名称
KubeletSkipVerify       bool   `json:"kubelet_skip_verify,omitempty"` // 是否跳过kubelet证书验证
SmartLimitUseKubeletAPI bool   `json:"smart_limit_use_kubelet_api"`   // 是否使用kubelet API获取IO数据

	// 解除限速配置
	SmartLimitRemoveThreshold     float64 `json:"smart_limit_remove_threshold"`      // 解除限速阈值（相对值）
	SmartLimitRemoveDelay         int     `json:"smart_limit_remove_delay"`          // 解除限速延迟（分钟）
	SmartLimitRemoveCheckInterval int     `json:"smart_limit_remove_check_interval"` // 解除限速检查间隔（分钟）

	// 新增全局默认和最大限额配置
	DefaultIOPSLimit int `yaml:"default_iops_limit" json:"default_iops_limit"`
	DefaultBPSLimit  int `yaml:"default_bps_limit" json:"default_bps_limit"`
	MaxIOPSLimit     int `yaml:"max_iops_limit" json:"max_iops_limit"`
	MaxBPSLimit      int `yaml:"max_bps_limit" json:"max_bps_limit"`
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *Config {
	return &Config{
		ContainerIOPSLimit:            500,
		ContainerReadIOPSLimit:        500,
		ContainerWriteIOPSLimit:       500,
		ContainerReadBPSLimit:         0, // 默认不限制读
		ContainerWriteBPSLimit:        0, // 默认不限制写
		DataMount:                     "/data",
		ExcludeKeywords:               []string{"pause", "istio-proxy", "psmdb", "kube-system", "koordinator", "apisix"},
		ExcludeNamespaces:             []string{"kube-system"},
		ExcludeLabelSelector:          "",
		ContainerdNamespace:           "k8s.io",
		ContainerRuntime:              "auto",
		CgroupVersion:                 "auto",
		ContainerSocketPath:           "/run/containerd/containerd.sock",
		KubeletHost:                   "localhost",
		KubeletPort:                   "10250",
		KubeConfigPath:                "",
		SmartLimitEnabled:             false,
		SmartLimitMonitorInterval:     60,
		SmartLimitHistoryWindow:       10,
		SmartLimitHighIOThreshold:     0.8,
		SmartLimitHighBPSThreshold:    0.8,
		SmartLimitAutoIOPS:            0,
		SmartLimitAutoBPS:             0,
		SmartLimitAnnotationPrefix:    "kubediskguard.io",
		KubeletTokenPath:              "",
		KubeletCAPath:                 "",
		KubeletSkipVerify:             false,
		SmartLimitUseKubeletAPI:       true, // 默认启用kubelet API
		SmartLimitGradedThresholds:    false,
		SmartLimitIOThreshold15m:      0.8,
		SmartLimitBPSThreshold15m:     0.8,
		SmartLimitIOPSLimit15m:        0,
		SmartLimitBPSLimit15m:         0,
		SmartLimitIOThreshold30m:      0.8,
		SmartLimitBPSThreshold30m:     0.8,
		SmartLimitIOPSLimit30m:        0,
		SmartLimitBPSLimit30m:         0,
		SmartLimitIOThreshold60m:      0.8,
		SmartLimitBPSThreshold60m:     0.8,
		SmartLimitIOPSLimit60m:        0,
		SmartLimitBPSLimit60m:         0,
		SmartLimitRemoveThreshold:     0.5,
		SmartLimitRemoveDelay:         5,
		SmartLimitRemoveCheckInterval: 1,
		DefaultIOPSLimit:              500,
		DefaultBPSLimit:               10 * 1024 * 1024, // 10MB
		MaxIOPSLimit:                  2000,
		MaxBPSLimit:                   100 * 1024 * 1024, // 100MB
	}
}

// LoadFromEnv 从环境变量加载配置
func LoadFromEnv(config *Config) {
	if val := os.Getenv("CONTAINER_IOPS_LIMIT"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.ContainerIOPSLimit = iops
		}
	}
	if val := os.Getenv("CONTAINER_READ_IOPS_LIMIT"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.ContainerReadIOPSLimit = iops
		}
	}
	if val := os.Getenv("CONTAINER_WRITE_IOPS_LIMIT"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.ContainerWriteIOPSLimit = iops
		}
	}
	if val := os.Getenv("CONTAINER_READ_BPS_LIMIT"); val != "" {
		if bps, err := strconv.Atoi(val); err == nil {
			config.ContainerReadBPSLimit = bps
		}
	}
	if val := os.Getenv("CONTAINER_WRITE_BPS_LIMIT"); val != "" {
		if bps, err := strconv.Atoi(val); err == nil {
			config.ContainerWriteBPSLimit = bps
		}
	}

	if val := os.Getenv("DATA_MOUNT"); val != "" {
		config.DataMount = val
	}

	if val := os.Getenv("EXCLUDE_KEYWORDS"); val != "" {
		config.ExcludeKeywords = strings.Split(val, ",")
	}

	if val := os.Getenv("EXCLUDE_NAMESPACES"); val != "" {
		config.ExcludeNamespaces = strings.Split(val, ",")
	}

	if val := os.Getenv("EXCLUDE_LABEL_SELECTOR"); val != "" {
		config.ExcludeLabelSelector = val
	}

	if val := os.Getenv("CONTAINERD_NAMESPACE"); val != "" {
		config.ContainerdNamespace = val
	}

	if val := os.Getenv("CONTAINER_RUNTIME"); val != "" {
		config.ContainerRuntime = val
	}

	if val := os.Getenv("CGROUP_VERSION"); val != "" {
		config.CgroupVersion = val
	}

	if val := os.Getenv("CONTAINER_SOCKET_PATH"); val != "" {
		config.ContainerSocketPath = val
	}

	if val := os.Getenv("KUBELET_HOST"); val != "" {
		config.KubeletHost = val
	}

	if val := os.Getenv("KUBELET_PORT"); val != "" {
		config.KubeletPort = val
	}

	if v := os.Getenv("KUBECONFIG_PATH"); v != "" {
		config.KubeConfigPath = v
	}

	if val := os.Getenv("SMART_LIMIT_ENABLED"); val != "" {
		if enabled, err := strconv.ParseBool(val); err == nil {
			config.SmartLimitEnabled = enabled
		}
	}

	if val := os.Getenv("SMART_LIMIT_MONITOR_INTERVAL"); val != "" {
		if interval, err := strconv.Atoi(val); err == nil {
			config.SmartLimitMonitorInterval = interval
		}
	}

	if val := os.Getenv("SMART_LIMIT_HISTORY_WINDOW"); val != "" {
		if window, err := strconv.Atoi(val); err == nil {
			config.SmartLimitHistoryWindow = window
		}
	}

	if val := os.Getenv("SMART_LIMIT_HIGH_IO_THRESHOLD"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitHighIOThreshold = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_HIGH_BPS_THRESHOLD"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitHighBPSThreshold = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_AUTO_IOPS"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.SmartLimitAutoIOPS = iops
		}
	}

	if val := os.Getenv("SMART_LIMIT_AUTO_BPS"); val != "" {
		if bps, err := strconv.Atoi(val); err == nil {
			config.SmartLimitAutoBPS = bps
		}
	}

	if val := os.Getenv("SMART_LIMIT_ANNOTATION_PREFIX"); val != "" {
		config.SmartLimitAnnotationPrefix = val
	}

	if val := os.Getenv("KUBELET_TOKEN_PATH"); val != "" {
		config.KubeletTokenPath = val
	}

	if val := os.Getenv("KUBELET_CA_PATH"); val != "" {
		config.KubeletCAPath = val
	}

	if val := os.Getenv("KUBELET_SERVER_NAME"); val != "" {
		config.KubeletServerName = val
	}

	if val := os.Getenv("KUBELET_SKIP_VERIFY"); val != "" {
		if skipVerify, err := strconv.ParseBool(val); err == nil {
			config.KubeletSkipVerify = skipVerify
		}
	}

	if val := os.Getenv("SMART_LIMIT_USE_KUBELET_API"); val != "" {
		if useKubeletAPI, err := strconv.ParseBool(val); err == nil {
			config.SmartLimitUseKubeletAPI = useKubeletAPI
		}
	}

	if val := os.Getenv("SMART_LIMIT_GRADED_THRESHOLDS"); val != "" {
		if gradedThresholds, err := strconv.ParseBool(val); err == nil {
			config.SmartLimitGradedThresholds = gradedThresholds
		}
	}

	if val := os.Getenv("SMART_LIMIT_IO_THRESHOLD_15M"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitIOThreshold15m = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_BPS_THRESHOLD_15M"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitBPSThreshold15m = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_IOPS_LIMIT_15M"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.SmartLimitIOPSLimit15m = iops
		}
	}

	if val := os.Getenv("SMART_LIMIT_BPS_LIMIT_15M"); val != "" {
		if bps, err := strconv.Atoi(val); err == nil {
			config.SmartLimitBPSLimit15m = bps
		}
	}

	if val := os.Getenv("SMART_LIMIT_IO_THRESHOLD_30M"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitIOThreshold30m = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_BPS_THRESHOLD_30M"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitBPSThreshold30m = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_IOPS_LIMIT_30M"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.SmartLimitIOPSLimit30m = iops
		}
	}

	if val := os.Getenv("SMART_LIMIT_BPS_LIMIT_30M"); val != "" {
		if bps, err := strconv.Atoi(val); err == nil {
			config.SmartLimitBPSLimit30m = bps
		}
	}

	if val := os.Getenv("SMART_LIMIT_IO_THRESHOLD_60M"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitIOThreshold60m = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_BPS_THRESHOLD_60M"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitBPSThreshold60m = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_IOPS_LIMIT_60M"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.SmartLimitIOPSLimit60m = iops
		}
	}

	if val := os.Getenv("SMART_LIMIT_BPS_LIMIT_60M"); val != "" {
		if bps, err := strconv.Atoi(val); err == nil {
			config.SmartLimitBPSLimit60m = bps
		}
	}

	if val := os.Getenv("SMART_LIMIT_REMOVE_THRESHOLD"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			config.SmartLimitRemoveThreshold = threshold
		}
	}

	if val := os.Getenv("SMART_LIMIT_REMOVE_DELAY"); val != "" {
		if delay, err := strconv.Atoi(val); err == nil {
			config.SmartLimitRemoveDelay = delay
		}
	}

	if val := os.Getenv("SMART_LIMIT_REMOVE_CHECK_INTERVAL"); val != "" {
		if interval, err := strconv.Atoi(val); err == nil {
			config.SmartLimitRemoveCheckInterval = interval
		}
	}
}

// ToJSON 将配置转换为JSON字符串
func (c *Config) ToJSON() string {
	configJSON, _ := json.MarshalIndent(c, "", "  ")
	return string(configJSON)
}
