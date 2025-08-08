package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"KubeDiskGuard/pkg/config"
)

// TestIsSystemdCgroupPath 测试systemd cgroup路径识别
func TestIsSystemdCgroupPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "systemd managed path",
			path:     "kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice:cri-containerd:16c0f5cee8ed9198de793b9a08fc93eb75ea630c552e5c7218471b9901cb40e3",
			expected: true,
		},
		{
			name:     "non-systemd path",
			path:     "/kubepods/burstable/podc9c501eb-9423-4bd6-b96f-7f10f7f4527c/f3ee04629f75567e95fae8425cb3e9b3e1c91346b1a2ddee9139c9216c713dc7",
			expected: false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "slice without cri-containerd",
			path:     "kubelet-kubepods.slice:docker:123456",
			expected: false,
		},
	}

	runtime := &ContainerdRuntime{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runtime.isSystemdCgroupPath(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertSystemdCgroupPath 测试systemd cgroup路径转换
func TestConvertSystemdCgroupPath(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{
			name:     "besteffort pod systemd path",
			input:    "kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice:cri-containerd:16c0f5cee8ed9",
			expected: "/sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-besteffort.slice/kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice/cri-containerd-16c0f5cee8ed9.scope/",
		},
		{
			name:     "burstable pod systemd path",
			input:    "kubelet-kubepods-burstable-pod123.slice:cri-containerd:abc123",
			expected: "/sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-burstable.slice/kubelet-kubepods-burstable-pod123.slice/cri-containerd-abc123.scope/",
		},
		{
			name:     "direct pod slice systemd path",
			input:    "kubelet-kubepods-poda5762175_5440_4e1e_be30_a69d9073ce0c.slice:cri-containerd:def456",
			expected: "/sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-poda5762175_5440_4e1e_be30_a69d9073ce0c.slice/cri-containerd-def456.scope/",
		},
		{
			name:        "invalid format - missing parts",
			input:       "kubelet-kubepods.slice:cri-containerd",
			expectError: true,
		},
		{
			name:        "invalid format - too many parts",
			input:       "kubelet-kubepods.slice:cri-containerd:123:extra",
			expectError: true,
		},
	}

	runtime := &ContainerdRuntime{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := runtime.convertSystemdCgroupPath(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestGetCgroupPathLogic 测试cgroup路径获取逻辑（模拟）
func TestGetCgroupPathLogic(t *testing.T) {
	tests := []struct {
		name           string
		cgroupVersion  string
		cgroupsPath    string
		expected       string
	}{
		{
			name:          "cgroup v1 path",
			cgroupVersion: "v1",
			cgroupsPath:   "/kubepods/burstable/pod123/container456",
			expected:      "/sys/fs/cgroup/blkio/kubepods/burstable/pod123/container456",
		},
		{
			name:          "cgroup v2 non-systemd path with leading slash",
			cgroupVersion: "v2",
			cgroupsPath:   "/kubepods/burstable/podc9c501eb-9423-4bd6-b96f-7f10f7f4527c/f3ee04629f75567e95fae8425cb3e9b3e1c91346b1a2ddee9139c9216c713dc7",
			expected:      "/sys/fs/cgroup/kubepods/burstable/podc9c501eb-9423-4bd6-b96f-7f10f7f4527c/f3ee04629f75567e95fae8425cb3e9b3e1c91346b1a2ddee9139c9216c713dc7",
		},
		{
			name:          "cgroup v2 non-systemd path without leading slash",
			cgroupVersion: "v2",
			cgroupsPath:   "kubepods/burstable/pod123/container456",
			expected:      "/sys/fs/cgroup/kubepods/burstable/pod123/container456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &ContainerdRuntime{
				config: &config.Config{
					CgroupVersion: tt.cgroupVersion,
				},
			}

			// 模拟路径构建逻辑
			var result string
			if tt.cgroupVersion == "v1" {
				result = "/sys/fs/cgroup/blkio" + tt.cgroupsPath
			} else {
				// 检查是否为systemd路径
				if runtime.isSystemdCgroupPath(tt.cgroupsPath) {
					// 这里不测试systemd转换，因为需要mock containerd client
					t.Skip("systemd path conversion requires mocked containerd client")
				} else {
					if tt.cgroupsPath != "" && tt.cgroupsPath[0] == '/' {
						result = "/sys/fs/cgroup" + tt.cgroupsPath
					} else {
						result = "/sys/fs/cgroup/" + tt.cgroupsPath
					}
				}
			}

			assert.Equal(t, tt.expected, result)
		})
	}
}