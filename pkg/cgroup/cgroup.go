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
		// cgroup v2: 检查容器是否仍在运行，如果已停止则跳过重置
		ioMaxFile := filepath.Join(cgroupPath, "io.max")

		// 首先检查cgroup目录是否存在
		if _, err := os.Stat(cgroupPath); os.IsNotExist(err) {
			log.Printf("Cgroup path %s does not exist, container may have been removed", cgroupPath)
			return nil
		}

		// 检查io.max文件是否存在
		if _, err := os.Stat(ioMaxFile); os.IsNotExist(err) {
			log.Printf("io.max file does not exist at %s, skipping reset", ioMaxFile)
			return nil
		}

		// 尝试多种重置方式
		resetValues := []string{
			"max",                         // 标准重置值
			fmt.Sprintf("%s max", majMin), // 带设备号的重置
			fmt.Sprintf("%s rbps=max wbps=max riops=max wiops=max", majMin), // 显式重置所有项
		}

		var lastErr error
		for i, resetValue := range resetValues {
			if err := os.WriteFile(ioMaxFile, []byte(resetValue), 0644); err != nil {
				lastErr = err
				log.Printf("Reset attempt %d failed with value '%s': %v", i+1, resetValue, err)
				continue
			}
			log.Printf("Successfully reset limits at %s (v2) with value: %s", majMin, resetValue)
			return nil
		}

		// 如果所有重置方式都失败，尝试读取当前值并记录
		if currentContent, err := os.ReadFile(ioMaxFile); err == nil {
			log.Printf("Current io.max content: %s", string(currentContent))
		}

		return fmt.Errorf("failed to reset io.max after all attempts: %v", lastErr)
	}
	return nil
}

// SetLimits 统一设置IOPS和BPS限制（riops/wiops/rbps/wbps），为0时写入<majMin> 0以解除该项限速
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
			if err := os.WriteFile(readIOPSFile, []byte(fmt.Sprintf("%s 0", majMin)), 0644); err != nil {
				return fmt.Errorf("failed to reset read iops limit: %v", err)
			}
		}
		writeIOPSFile := filepath.Join(cgroupPath, "blkio.throttle.write_iops_device")
		if wiops > 0 {
			if err := os.WriteFile(writeIOPSFile, []byte(fmt.Sprintf("%s %d", majMin, wiops)), 0644); err != nil {
				return fmt.Errorf("failed to set write iops limit: %v", err)
			}
		} else {
			if err := os.WriteFile(writeIOPSFile, []byte(fmt.Sprintf("%s 0", majMin)), 0644); err != nil {
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
			if err := os.WriteFile(readBPSFile, []byte(fmt.Sprintf("%s 0", majMin)), 0644); err != nil {
				return fmt.Errorf("failed to reset read bps limit: %v", err)
			}
		}
		writeBPSFile := filepath.Join(cgroupPath, "blkio.throttle.write_bps_device")
		if wbps > 0 {
			if err := os.WriteFile(writeBPSFile, []byte(fmt.Sprintf("%s %d", majMin, wbps)), 0644); err != nil {
				return fmt.Errorf("failed to set write bps limit: %v", err)
			}
		} else {
			if err := os.WriteFile(writeBPSFile, []byte(fmt.Sprintf("%s 0", majMin)), 0644); err != nil {
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
			if err := os.WriteFile(filepath.Join(cgroupPath, file), []byte(fmt.Sprintf("%s 0", majMin)), 0644); err != nil {
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
