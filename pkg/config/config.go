package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// WindowConfig 表示单个智能限速时间窗口的配置
// 支持自定义窗口长度、阈值
// 例如: {duration: 5, iops_threshold: 8000, bps_threshold: 8000}
type WindowConfig struct {
	Duration      int `json:"duation" yaml:"duation"` // 窗口长度（分钟）
	IOPSThreshold int `json:"iops_threshold" yaml:"iops_threshold"`
	BPSThreshold  int `json:"bps_threshold" yaml:"bps_threshold"`
}

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
	// kubelet API配置
	KubeletHost       string `json:"kubelet_host,omitempty"` // kubelet主机地址
	KubeletPort       string `json:"kubelet_port,omitempty"` // kubelet端口
	KubeConfigPath    string // 支持集群外部运行
	KubeletTokenPath  string `json:"kubelet_token_path,omitempty"`  // kubelet token路径
	KubeletCAPath     string `json:"kubelet_ca_path,omitempty"`     // kubelet CA证书路径
	KubeletSkipVerify bool   `json:"kubelet_skip_verify,omitempty"` // 是否跳过kubelet证书验证

	// 智能限速配置
	SmartLimitEnabled          bool   `json:"smart_limit_enabled"`
	SmartLimitMonitorInterval  int    `json:"smart_limit_monitor_interval"`  // 监控间隔（秒）
	SmartLimitHistoryWindow    int    `json:"smart_limit_history_window"`    // 历史数据保留窗口（分钟）
	SmartLimitAnnotationPrefix string `json:"smart_limit_annotation_prefix"` // 注解前缀
	// 智能限速多窗口自定义配置
	SmartLimitWindows []WindowConfig `json:"smart_limit_windows" yaml:"smart_limit_windows"` // 支持1~3个自定义窗口
	// 解除限速配置
	SmartLimitRemoveThreshold     float64 `json:"smart_limit_remove_threshold"`      // 解除限速阈值（相对值）
	SmartLimitRemoveDelay         int     `json:"smart_limit_remove_delay"`          // 解除限速延迟（分钟）
	SmartLimitRemoveCheckInterval int     `json:"smart_limit_remove_check_interval"` // 解除限速检查间隔（分钟）

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
		SmartLimitHistoryWindow:       60,
		SmartLimitAnnotationPrefix:    "io-limit",
		KubeletTokenPath:              "",
		KubeletCAPath:                 "",
		KubeletSkipVerify:             false,
		SmartLimitRemoveDelay:         5,
		SmartLimitRemoveCheckInterval: 1,
		SmartLimitWindows: []WindowConfig{
			{Duration: 1, IOPSThreshold: 500, BPSThreshold: 20 * 1024 * 1024},
			{Duration: 5, IOPSThreshold: 800, BPSThreshold: 30 * 1024 * 1024},
			{Duration: 30, IOPSThreshold: 1000, BPSThreshold: 50 * 1024 * 1024},
		},
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

	if val := os.Getenv("SMART_LIMIT_ANNOTATION_PREFIX"); val != "" {
		config.SmartLimitAnnotationPrefix = val
	}

	if val := os.Getenv("KUBELET_TOKEN_PATH"); val != "" {
		config.KubeletTokenPath = val
	}

	if val := os.Getenv("KUBELET_CA_PATH"); val != "" {
		config.KubeletCAPath = val
	}

	if val := os.Getenv("KUBELET_SKIP_VERIFY"); val != "" {
		if skipVerify, err := strconv.ParseBool(val); err == nil {
			config.KubeletSkipVerify = skipVerify
		}
	}

	if val := os.Getenv("SMART_LIMIT_HISTORY_WINDOW"); val != "" {
		if window, err := strconv.Atoi(val); err == nil {
			config.SmartLimitHistoryWindow = window
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
// ToJSON 将配置转换为JSON字符串（用于日志输出）
func (c *Config) ToJSON() string {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "配置序列化失败"
	}
	return string(data)
}
