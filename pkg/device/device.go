package device

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetMajMin 获取设备主次设备号
func GetMajMin(dataMount string) (string, error) {
	// 获取挂载点对应的设备
	cmd := exec.Command("df", dataMount)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get mount info: %v", err)
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("invalid df output")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 1 {
		return "", fmt.Errorf("invalid df output format")
	}

	device := fields[0]

	// 获取父设备
	cmd = exec.Command("lsblk", "-no", "PKNAME", device)
	output, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get parent device: %v", err)
	}

	parentDev := strings.TrimSpace(string(output))
	if parentDev == "" {
		return "", fmt.Errorf("no parent device found")
	}

	// 获取主次设备号
	cmd = exec.Command("lsblk", "-no", "MAJ:MIN", "/dev/"+parentDev)
	output, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get major:minor: %v", err)
	}

	majMin := strings.TrimSpace(string(output))
	if majMin == "" {
		return "", fmt.Errorf("no major:minor found")
	}

	return majMin, nil
}
