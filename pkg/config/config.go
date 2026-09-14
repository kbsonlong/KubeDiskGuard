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

	// 策略和对账配置
	AnnotationPrefix  string `json:"annotation_prefix"`  // Pod 注解前缀
	ReconcileInterval int    `json:"reconcile_interval"` // 全量对账间隔（秒）

	// kubelet API 配置（用于读取本节点 Pod；API Server 仍负责 watch）
	KubeletTokenPath  string `json:"kubelet_token_path,omitempty"`
	KubeletCAPath     string `json:"kubelet_ca_path,omitempty"`
	KubeletServerName string `json:"kubelet_server_name,omitempty"`
	KubeletSkipVerify bool   `json:"kubelet_skip_verify,omitempty"`
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *Config {
	return &Config{
		ContainerIOPSLimit:      500,
		ContainerReadIOPSLimit:  500,
		ContainerWriteIOPSLimit: 500,
		ContainerReadBPSLimit:   0, // 默认不限制读
		ContainerWriteBPSLimit:  0, // 默认不限制写
		DataMount:               "/data",
		ExcludeKeywords:         []string{"pause", "istio-proxy", "psmdb", "kube-system", "koordinator", "apisix"},
		ExcludeNamespaces:       []string{"kube-system"},
		ExcludeLabelSelector:    "",
		ContainerdNamespace:     "k8s.io",
		ContainerRuntime:        "auto",
		CgroupVersion:           "auto",
		ContainerSocketPath:     "/run/containerd/containerd.sock",
		KubeletHost:             "localhost",
		KubeletPort:             "10250",
		KubeConfigPath:          "",
		AnnotationPrefix:        "kubediskguard.io",
		ReconcileInterval:       300,
		KubeletTokenPath:        "",
		KubeletCAPath:           "",
		KubeletSkipVerify:       false,
	}
}

// LoadFromEnv 从环境变量加载配置
func LoadFromEnv(config *Config) {
	if val := os.Getenv("CONTAINER_IOPS_LIMIT"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.ContainerIOPSLimit = iops
			// 兼容统一配置；后续的读写专属环境变量仍可覆盖对应方向。
			config.ContainerReadIOPSLimit = iops
			config.ContainerWriteIOPSLimit = iops
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

	if val := os.Getenv("ANNOTATION_PREFIX"); val != "" {
		config.AnnotationPrefix = val
	} else if val := os.Getenv("SMART_LIMIT_ANNOTATION_PREFIX"); val != "" {
		// 保留旧环境变量，避免升级时中断既有注解策略。
		config.AnnotationPrefix = val
	}

	if val := os.Getenv("RECONCILE_INTERVAL"); val != "" {
		if interval, err := strconv.Atoi(val); err == nil && interval > 0 {
			config.ReconcileInterval = interval
		}
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

}

// ToJSON 将配置转换为JSON字符串
func (c *Config) ToJSON() string {
	configJSON, _ := json.MarshalIndent(c, "", "  ")
	return string(configJSON)
}
