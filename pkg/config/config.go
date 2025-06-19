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
	CheckInterval           int      `json:"check_interval"`
	ContainerSocketPath     string   `json:"container_socket_path,omitempty"` // 可选字段，默认为空
	KubeletHost             string   `json:"kubelet_host,omitempty"`          // kubelet主机地址
	KubeletPort             string   `json:"kubelet_port,omitempty"`          // kubelet端口
	KubeConfigPath          string   // 支持集群外部运行
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *Config {
	return &Config{
		ContainerIOPSLimit:      500,
		ContainerReadIOPSLimit:  500,
		ContainerWriteIOPSLimit: 500,
		ContainerReadBPSLimit:   1048576, // 1MB/s
		ContainerWriteBPSLimit:  1048576, // 1MB/s
		DataMount:               "/data",
		ExcludeKeywords:         []string{"pause", "istio-proxy", "psmdb", "kube-system", "koordinator", "apisix"},
		ExcludeNamespaces:       []string{"kube-system"},
		ExcludeLabelSelector:    "",
		ContainerdNamespace:     "k8s.io",
		ContainerRuntime:        "auto",
		CgroupVersion:           "auto",
		CheckInterval:           30,
		ContainerSocketPath:     "/run/containerd/containerd.sock",
		KubeletHost:             "",
		KubeletPort:             "",
		KubeConfigPath:          "",
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

	if val := os.Getenv("CHECK_INTERVAL"); val != "" {
		if interval, err := strconv.Atoi(val); err == nil {
			config.CheckInterval = interval
		}
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
}

// ToJSON 将配置转换为JSON字符串
func (c *Config) ToJSON() string {
	configJSON, _ := json.MarshalIndent(c, "", "  ")
	return string(configJSON)
}
