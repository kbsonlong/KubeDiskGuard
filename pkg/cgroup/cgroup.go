package cgroup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// ReadIOPressure 读取IO压力（PSI），返回avg10和avg60
// v1: 读取/proc/pressure/io（节点级回退）
// v2: 读取<cgroupPath>/io.pressure（容器级）
func (m *Manager) ReadIOPressure(cgroupPath string) (float64, float64, error) {
	var avg10, avg60 float64
	var data []byte
	var err error
	if m.version == "v1" {
		data, err = os.ReadFile("/proc/pressure/io")
		if err != nil {
			return 0, 0, err
		}
	} else {
		p := filepath.Join(cgroupPath, "io.pressure")
		data, err = os.ReadFile(p)
		if err != nil {
			return 0, 0, err
		}
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "some") {
			fields := strings.Fields(line)
			for _, f := range fields {
				if strings.HasPrefix(f, "avg10=") {
					fmt.Sscanf(f, "avg10=%f", &avg10)
				}
				if strings.HasPrefix(f, "avg60=") {
					fmt.Sscanf(f, "avg60=%f", &avg60)
				}
			}
			break
		}
	}
	return avg10, avg60, nil
}

type V1ThrottleDetail struct {
	ReadDeltaIOPS   float64
	WriteDeltaIOPS  float64
	ReadDeltaBPS    float64
	WriteDeltaBPS   float64
	ReadLimitIOPS   float64
	WriteLimitIOPS  float64
	ReadLimitBPS    float64
	WriteLimitBPS   float64
}

func (m *Manager) DetectV1Throttle(cgroupPath, majMin string, interval time.Duration, epsilon float64) (bool, V1ThrottleDetail, error) {
	var d V1ThrottleDetail
	if m.version != "v1" {
		return false, d, fmt.Errorf("not v1")
	}
	sReadOps0, sWriteOps0, sReadBytes0, sWriteBytes0, err := readV1BlkioSnapshot(cgroupPath)
	if err != nil {
		return false, d, err
	}
	time.Sleep(interval)
	sReadOps1, sWriteOps1, sReadBytes1, sWriteBytes1, err := readV1BlkioSnapshot(cgroupPath)
	if err != nil {
		return false, d, err
	}
	riops := readV1Limit(filepath.Join(cgroupPath, "blkio.throttle.read_iops_device"), majMin)
	wiops := readV1Limit(filepath.Join(cgroupPath, "blkio.throttle.write_iops_device"), majMin)
	rbps := readV1Limit(filepath.Join(cgroupPath, "blkio.throttle.read_bps_device"), majMin)
	wbps := readV1Limit(filepath.Join(cgroupPath, "blkio.throttle.write_bps_device"), majMin)
	d.ReadDeltaIOPS = float64(sReadOps1 - sReadOps0) / interval.Seconds()
	d.WriteDeltaIOPS = float64(sWriteOps1 - sWriteOps0) / interval.Seconds()
	d.ReadDeltaBPS = float64(sReadBytes1 - sReadBytes0) / interval.Seconds()
	d.WriteDeltaBPS = float64(sWriteBytes1 - sWriteBytes0) / interval.Seconds()
	d.ReadLimitIOPS = float64(riops)
	d.WriteLimitIOPS = float64(wiops)
	d.ReadLimitBPS = float64(rbps)
	d.WriteLimitBPS = float64(wbps)
	trRead := rbps > 0 && d.ReadDeltaBPS >= float64(rbps)*epsilon
	trWrite := wbps > 0 && d.WriteDeltaBPS >= float64(wbps)*epsilon
	tiRead := riops > 0 && d.ReadDeltaIOPS >= float64(riops)*epsilon
	tiWrite := wiops > 0 && d.WriteDeltaIOPS >= float64(wiops)*epsilon
	return trRead || trWrite || tiRead || tiWrite, d, nil
}

func readV1BlkioSnapshot(cgroupPath string) (uint64, uint64, uint64, uint64, error) {
	opsFile := filepath.Join(cgroupPath, "blkio.throttle.io_serviced_recursive")
	bytesFile := filepath.Join(cgroupPath, "blkio.throttle.io_service_bytes_recursive")
	readOps, writeOps, err := parseV1Ops(opsFile)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	readBytes, writeBytes, err := parseV1Bytes(bytesFile)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return readOps, writeOps, readBytes, writeBytes, nil
}

func parseV1Ops(path string) (uint64, uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var r, w uint64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		if f[1] == "Read" {
			if v, err := strconv.ParseUint(f[2], 10, 64); err == nil {
				r += v
			}
		}
		if f[1] == "Write" {
			if v, err := strconv.ParseUint(f[2], 10, 64); err == nil {
				w += v
			}
		}
	}
	return r, w, nil
}

func parseV1Bytes(path string) (uint64, uint64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var r, w uint64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		if f[1] == "Read" {
			if v, err := strconv.ParseUint(f[2], 10, 64); err == nil {
				r += v
			}
		}
		if f[1] == "Write" {
			if v, err := strconv.ParseUint(f[2], 10, 64); err == nil {
				w += v
			}
		}
	}
	return r, w, nil
}

func readV1Limit(path, majMin string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		if f[0] == majMin {
			if v, err := strconv.ParseUint(f[1], 10, 64); err == nil {
				return v
			}
		}
	}
	return 0
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
