package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

// Config 配置结构体
type Config struct {
	ContainerIOPSLimit   int      `json:"container_iops_limit"`
	DataTotalIOPS        int      `json:"data_total_iops"`
	DataMount            string   `json:"data_mount"`
	ExcludeKeywords      []string `json:"exclude_keywords"`
	ExcludeNamespaces    []string `json:"exclude_namespaces"`
	ExcludeRegexps       []string `json:"exclude_regexps"`
	ExcludeLabelSelector string   `json:"exclude_label_selector"`
	ContainerdNamespace  string   `json:"containerd_namespace"`
	ContainerRuntime     string   `json:"container_runtime"`
	CgroupVersion        string   `json:"cgroup_version"`
	CheckInterval        int      `json:"check_interval"`
	ContainerSocketPath  string   `json:"container_socket_path,omitempty"` // 可选字段，默认为空
}

// GetDefaultConfig 获取默认配置
func GetDefaultConfig() *Config {
	return &Config{
		ContainerIOPSLimit:   500,
		DataTotalIOPS:        3000,
		DataMount:            "/data",
		ExcludeKeywords:      []string{"pause", "istio-proxy", "psmdb", "kube-system", "koordinator", "apisix"},
		ExcludeNamespaces:    []string{"kube-system"},
		ExcludeRegexps:       []string{},
		ExcludeLabelSelector: "",
		ContainerdNamespace:  "k8s.io",
		ContainerRuntime:     "auto",
		CgroupVersion:        "auto",
		CheckInterval:        30,
		ContainerSocketPath:  "/run/containerd/containerd.sock",
	}
}

// LoadFromEnv 从环境变量加载配置
func LoadFromEnv(config *Config) {
	if val := os.Getenv("CONTAINER_IOPS_LIMIT"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.ContainerIOPSLimit = iops
		}
	}

	if val := os.Getenv("DATA_TOTAL_IOPS"); val != "" {
		if iops, err := strconv.Atoi(val); err == nil {
			config.DataTotalIOPS = iops
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

	if val := os.Getenv("EXCLUDE_REGEXPS"); val != "" {
		config.ExcludeRegexps = strings.Split(val, ",")
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

	if val := os.Getenv("Container_Socket_Path"); val != "" {
		config.ContainerSocketPath = val
	}
}

// ToJSON 将配置转换为JSON字符串
func (c *Config) ToJSON() string {
	configJSON, _ := json.MarshalIndent(c, "", "  ")
	return string(configJSON)
}
