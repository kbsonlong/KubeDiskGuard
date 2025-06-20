package cgroup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

		log.Printf("Set IOPS limit at %s %s (v1)", majMin, iopsLimitStr)
	} else {
		// cgroup v2: 写入io.max文件
		ioMaxFile := filepath.Join(cgroupPath, "io.max")
		content := fmt.Sprintf("%s riops=%s wiops=%s", majMin, iopsLimitStr, iopsLimitStr)

		if err := os.WriteFile(ioMaxFile, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to set io.max: %v", err)
		}

		log.Printf("Set IOPS limit at %s %s (v2)", majMin, iopsLimitStr)
	}

	return nil
}

// ResetIOPSLimit 解除IOPS限制
func (m *Manager) ResetIOPSLimit(cgroupPath, majMin string) error {
	if cgroupPath == "" || majMin == "" {
		return fmt.Errorf("invalid cgroup path or major:minor")
	}
	if m.version == "v1" {
		readFile := filepath.Join(cgroupPath, "blkio.throttle.read_iops_device")
		writeFile := filepath.Join(cgroupPath, "blkio.throttle.write_iops_device")
		if err := os.WriteFile(readFile, []byte(""), 0644); err != nil {
			return fmt.Errorf("failed to reset read iops limit: %v", err)
		}
		if err := os.WriteFile(writeFile, []byte(""), 0644); err != nil {
			return fmt.Errorf("failed to reset write iops limit: %v", err)
		}
		log.Printf("Reset IOPS limit at %s (v1)", majMin)
	} else {
		ioMaxFile := filepath.Join(cgroupPath, "io.max")
		if err := os.WriteFile(ioMaxFile, []byte("default\n"), 0644); err != nil {
			return fmt.Errorf("failed to reset io.max: %v", err)
		}
		log.Printf("Reset IOPS limit at %s (v2)", majMin)
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

// SetBPSLimit 设置带宽限制（字节/秒）
func (m *Manager) SetBPSLimit(cgroupPath, majMin string, readBps, writeBps int) error {
	if cgroupPath == "" || majMin == "" {
		return fmt.Errorf("invalid cgroup path or major:minor")
	}
	if m.version == "v1" {
		if readBps > 0 {
			readFile := filepath.Join(cgroupPath, "blkio.throttle.read_bps_device")
			if err := os.WriteFile(readFile, []byte(fmt.Sprintf("%s %d", majMin, readBps)), 0644); err != nil {
				return fmt.Errorf("failed to set read bps limit: %v", err)
			}
		}
		if writeBps > 0 {
			writeFile := filepath.Join(cgroupPath, "blkio.throttle.write_bps_device")
			if err := os.WriteFile(writeFile, []byte(fmt.Sprintf("%s %d", majMin, writeBps)), 0644); err != nil {
				return fmt.Errorf("failed to set write bps limit: %v", err)
			}
		}
		log.Printf("Set BPS limit at %s rbps=%d wbps=%d (v1)", majMin, readBps, writeBps)
	} else {
		// cgroup v2
		ioMaxFile := filepath.Join(cgroupPath, "io.max")
		var content string
		if readBps > 0 && writeBps > 0 {
			content = fmt.Sprintf("%s rbps=%d wbps=%d", majMin, readBps, writeBps)
		} else if readBps > 0 {
			content = fmt.Sprintf("%s rbps=%d", majMin, readBps)
		} else if writeBps > 0 {
			content = fmt.Sprintf("%s wbps=%d", majMin, writeBps)
		} else {
			return nil
		}
		if err := os.WriteFile(ioMaxFile, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to set io.max bps: %v", err)
		}
		log.Printf("Set BPS limit at %s %s (v2)", majMin, content)
	}
	return nil
}

// ResetBPSLimit 解除带宽限速
func (m *Manager) ResetBPSLimit(cgroupPath, majMin string) error {
	if cgroupPath == "" || majMin == "" {
		return fmt.Errorf("invalid cgroup path or major:minor")
	}
	if m.version == "v1" {
		readFile := filepath.Join(cgroupPath, "blkio.throttle.read_bps_device")
		writeFile := filepath.Join(cgroupPath, "blkio.throttle.write_bps_device")
		if err := os.WriteFile(readFile, []byte{}, 0644); err != nil {
			return fmt.Errorf("failed to reset read bps: %v", err)
		}
		if err := os.WriteFile(writeFile, []byte{}, 0644); err != nil {
			return fmt.Errorf("failed to reset write bps: %v", err)
		}
		log.Printf("Reset BPS limit at %s (v1)", majMin)
	} else {
		ioMaxFile := filepath.Join(cgroupPath, "io.max")
		// v2: 写空或"max"
		if err := os.WriteFile(ioMaxFile, []byte("max"), 0644); err != nil {
			return fmt.Errorf("failed to reset io.max bps: %v", err)
		}
		log.Printf("Reset BPS limit at %s (v2)", majMin)
	}
	return nil
}

// SetLimits 统一设置IOPS和BPS限制（riops/wiops/rbps/wbps），为0时自动解除该项限速
func (m *Manager) SetLimits(cgroupPath, majMin string, riops, wiops, rbps, wbps int) error {
	if cgroupPath == "" || majMin == "" {
		return fmt.Errorf("invalid cgroup path or major:minor")
	}
	if m.version == "v1" {
		// IOPS
		readIOPSFile := filepath.Join(cgroupPath, "blkio.throttle.read_iops_device")
		if riops > 0 {
			if err := os.WriteFile(readIOPSFile, []byte(fmt.Sprintf("%s %d", majMin, riops)), 0644); err != nil {
				return fmt.Errorf("failed to set read iops limit: %v", err)
			}
		} else {
			if err := os.WriteFile(readIOPSFile, []byte{}, 0644); err != nil {
				return fmt.Errorf("failed to reset read iops limit: %v", err)
			}
		}
		writeIOPSFile := filepath.Join(cgroupPath, "blkio.throttle.write_iops_device")
		if wiops > 0 {
			if err := os.WriteFile(writeIOPSFile, []byte(fmt.Sprintf("%s %d", majMin, wiops)), 0644); err != nil {
				return fmt.Errorf("failed to set write iops limit: %v", err)
			}
		} else {
			if err := os.WriteFile(writeIOPSFile, []byte{}, 0644); err != nil {
				return fmt.Errorf("failed to reset write iops limit: %v", err)
			}
		}
		// BPS
		readBPSFile := filepath.Join(cgroupPath, "blkio.throttle.read_bps_device")
		if rbps > 0 {
			if err := os.WriteFile(readBPSFile, []byte(fmt.Sprintf("%s %d", majMin, rbps)), 0644); err != nil {
				return fmt.Errorf("failed to set read bps limit: %v", err)
			}
		} else {
			fmt.Println("Resetting read bps limit to empty")
			// 解除读带宽限速
			if err := os.WriteFile(readBPSFile, []byte{}, 0644); err != nil {
				return fmt.Errorf("failed to reset read bps limit: %v", err)
			}
		}
		writeBPSFile := filepath.Join(cgroupPath, "blkio.throttle.write_bps_device")
		if wbps > 0 {
			if err := os.WriteFile(writeBPSFile, []byte(fmt.Sprintf("%s %d", majMin, wbps)), 0644); err != nil {
				return fmt.Errorf("failed to set write bps limit: %v", err)
			}
		} else {
			if err := os.WriteFile(writeBPSFile, []byte{}, 0644); err != nil {
				return fmt.Errorf("failed to reset write bps limit: %v", err)
			}
		}
		log.Printf("Set limits at %s riops=%d wiops=%d rbps=%d wbps=%d (v1)", majMin, riops, wiops, rbps, wbps)
	} else {
		// cgroup v2: 一次性写入所有项，0项不写
		var parts []string
		if riops > 0 {
			parts = append(parts, fmt.Sprintf("riops=%d", riops))
		}
		if wiops > 0 {
			parts = append(parts, fmt.Sprintf("wiops=%d", wiops))
		}
		if rbps > 0 {
			parts = append(parts, fmt.Sprintf("rbps=%d", rbps))
		}
		if wbps > 0 {
			parts = append(parts, fmt.Sprintf("wbps=%d", wbps))
		}
		ioMaxFile := filepath.Join(cgroupPath, "io.max")
		if len(parts) == 0 {
			// 全部为0，解除所有限速
			if err := os.WriteFile(ioMaxFile, []byte("max"), 0644); err != nil {
				return fmt.Errorf("failed to reset io.max: %v", err)
			}
			log.Printf("Reset all limits at %s (v2)", majMin)
			return nil
		}
		content := fmt.Sprintf("%s %s", majMin, strings.Join(parts, " "))
		if err := os.WriteFile(ioMaxFile, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to set io.max: %v", err)
		}
		log.Printf("Set limits at %s %s (v2)", majMin, content)
	}
	return nil
}

// ResetLimits 统一解除所有IOPS和BPS限速
func (m *Manager) ResetLimits(cgroupPath, majMin string) error {
	if cgroupPath == "" || majMin == "" {
		return fmt.Errorf("invalid cgroup path or major:minor")
	}
	if m.version == "v1" {
		for _, file := range []string{
			"blkio.throttle.read_iops_device",
			"blkio.throttle.write_iops_device",
			"blkio.throttle.read_bps_device",
			"blkio.throttle.write_bps_device",
		} {
			if err := os.WriteFile(filepath.Join(cgroupPath, file), []byte{}, 0644); err != nil {
				return fmt.Errorf("failed to reset %s: %v", file, err)
			}
		}
		log.Printf("Reset all limits at %s (v1)", majMin)
	} else {
		ioMaxFile := filepath.Join(cgroupPath, "io.max")
		if err := os.WriteFile(ioMaxFile, []byte("max"), 0644); err != nil {
			return fmt.Errorf("failed to reset io.max: %v", err)
		}
		log.Printf("Reset all limits at %s (v2)", majMin)
	}
	return nil
}
