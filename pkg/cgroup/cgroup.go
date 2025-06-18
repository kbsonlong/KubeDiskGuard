package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Manager cgroup管理器
type Manager struct {
	version string
}

// NewManager 创建cgroup管理器
func NewManager(version string) *Manager {
	return &Manager{
		version: version,
	}
}

// FindCgroupPath 查找cgroup路径
func (m *Manager) FindCgroupPath(containerID string) string {
	if m.version == "v1" {
		// 查找blkio cgroup路径
		pattern := filepath.Join("/sys/fs/cgroup/blkio", "*"+containerID+"*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return matches[0]
		}
	} else {
		// cgroup v2
		pattern := filepath.Join("/sys/fs/cgroup", "*"+containerID+"*")
		matches, _ := filepath.Glob(pattern)
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return ""
}

// SetIOPSLimit 设置IOPS限制
func (m *Manager) SetIOPSLimit(cgroupPath, majMin string, iopsLimit int) error {
	if cgroupPath == "" || majMin == "" {
		return fmt.Errorf("invalid cgroup path or major:minor")
	}

	iopsLimitStr := strconv.Itoa(iopsLimit)

	if m.version == "v1" {
		// cgroup v1: 写入blkio.throttle文件
		readFile := filepath.Join(cgroupPath, "blkio.throttle.read_iops_device")
		writeFile := filepath.Join(cgroupPath, "blkio.throttle.write_iops_device")

		if err := os.WriteFile(readFile, []byte(majMin+" "+iopsLimitStr), 0644); err != nil {
			return fmt.Errorf("failed to set read iops limit: %v", err)
		}

		if err := os.WriteFile(writeFile, []byte(majMin+" "+iopsLimitStr), 0644); err != nil {
			return fmt.Errorf("failed to set write iops limit: %v", err)
		}

		fmt.Printf("[%s] Set IOPS limit at %s: %s %s (v1)\n",
			time.Now().Format("2006-01-02 15:04:05"), cgroupPath, majMin, iopsLimitStr)
	} else {
		// cgroup v2: 写入io.max文件
		ioMaxFile := filepath.Join(cgroupPath, "io.max")
		content := fmt.Sprintf("%s riops=%s wiops=%s", majMin, iopsLimitStr, iopsLimitStr)

		if err := os.WriteFile(ioMaxFile, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to set io.max: %v", err)
		}

		fmt.Printf("[%s] Set IOPS limit at %s: %s riops=%s wiops=%s (v2)\n",
			time.Now().Format("2006-01-02 15:04:05"), cgroupPath, majMin, iopsLimitStr, iopsLimitStr)
	}

	return nil
}

// BuildCgroupPath 构建cgroup路径
func (m *Manager) BuildCgroupPath(containerID, cgroupParent string) string {
	if m.version == "v1" {
		if cgroupParent == "" || cgroupParent == "/" {
			return filepath.Join("/sys/fs/cgroup/blkio/docker", containerID)
		} else {
			cgroupParentClean := cgroupParent
			if len(cgroupParent) > 0 && cgroupParent[0] == '/' {
				cgroupParentClean = cgroupParent[1:]
			}
			return filepath.Join("/sys/fs/cgroup/blkio", cgroupParentClean, containerID)
		}
	} else {
		return m.FindCgroupPath(containerID)
	}
}
