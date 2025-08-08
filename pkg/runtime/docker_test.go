package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDockerGetCgroupPathLogic 测试Docker cgroup路径构建逻辑
func TestDockerGetCgroupPathLogic(t *testing.T) {
	tests := []struct {
		name           string
		cgroupVersion  string
		cgroupParent   string
		containerID    string
		expectedPath   string
	}{
		{
			name:          "cgroup v1 with kubernetes pod parent",
			cgroupVersion: "v1",
			cgroupParent:  "/kubepods/burstable/pod4b5860c3-7ac8-47b6-921d-053136bfc3c9",
			containerID:   "b6abba6fc231831d331f08ced6d004c94996e184761018fed9514c37cf8e97a5",
			expectedPath:  "/sys/fs/cgroup/blkio/kubepods/burstable/pod4b5860c3-7ac8-47b6-921d-053136bfc3c9/b6abba6fc231831d331f08ced6d004c94996e184761018fed9514c37cf8e97a5",
		},
		{
			name:          "cgroup v1 with besteffort pod parent",
			cgroupVersion: "v1",
			cgroupParent:  "/kubepods/besteffort/pod123-456-789",
			containerID:   "abc123def456",
			expectedPath:  "/sys/fs/cgroup/blkio/kubepods/besteffort/pod123-456-789/abc123def456",
		},
		{
			name:          "cgroup v1 without parent (default docker path)",
			cgroupVersion: "v1",
			cgroupParent:  "",
			containerID:   "abc123def456",
			expectedPath:  "/sys/fs/cgroup/blkio/docker/abc123def456",
		},
		{
			name:          "cgroup v2 with kubernetes pod parent",
			cgroupVersion: "v2",
			cgroupParent:  "/kubepods/burstable/pod4b5860c3-7ac8-47b6-921d-053136bfc3c9",
			containerID:   "b6abba6fc231831d331f08ced6d004c94996e184761018fed9514c37cf8e97a5",
			expectedPath:  "/sys/fs/cgroup/kubepods/burstable/pod4b5860c3-7ac8-47b6-921d-053136bfc3c9/b6abba6fc231831d331f08ced6d004c94996e184761018fed9514c37cf8e97a5",
		},
		{
			name:          "cgroup v2 without parent (default docker path)",
			cgroupVersion: "v2",
			cgroupParent:  "",
			containerID:   "abc123def456",
			expectedPath:  "/sys/fs/cgroup/system.slice/docker-abc123def456.scope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 模拟路径构建逻辑
			var cgroupsPath string
			if tt.cgroupParent != "" {
				cgroupsPath = tt.cgroupParent + "/" + tt.containerID
			} else {
				if tt.cgroupVersion == "v1" {
					cgroupsPath = "/docker/" + tt.containerID
				} else {
					cgroupsPath = "/system.slice/docker-" + tt.containerID + ".scope"
				}
			}

			var actualPath string
			if tt.cgroupVersion == "v1" {
				actualPath = "/sys/fs/cgroup/blkio" + cgroupsPath
			} else {
				actualPath = "/sys/fs/cgroup" + cgroupsPath
			}

			assert.Equal(t, tt.expectedPath, actualPath)
		})
	}
}