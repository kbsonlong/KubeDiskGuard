package service

import (
	"fmt"
	"log"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/detector"
	"iops-limit-service/pkg/runtime"
)

// IOPSLimitService IOPS限制服务
type IOPSLimitService struct {
	config  *config.Config
	runtime container.Runtime
}

// NewIOPSLimitService 创建IOPS限制服务
func NewIOPSLimitService(config *config.Config) (*IOPSLimitService, error) {
	service := &IOPSLimitService{
		config: config,
	}

	// 自动检测运行时
	if config.ContainerRuntime == "auto" {
		config.ContainerRuntime = detector.DetectRuntime()
	}

	// 自动检测cgroup版本
	if config.CgroupVersion == "auto" {
		config.CgroupVersion = detector.DetectCgroupVersion()
	}

	log.Printf("Using container runtime: %s", config.ContainerRuntime)
	log.Printf("Detected cgroup version: %s", config.CgroupVersion)

	// 初始化运行时
	var err error
	if config.ContainerRuntime == "docker" {
		service.runtime, err = runtime.NewDockerRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker runtime: %v", err)
		}
	} else if config.ContainerRuntime == "containerd" {
		service.runtime, err = runtime.NewContainerdRuntime(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create containerd runtime: %v", err)
		}
	} else {
		return nil, fmt.Errorf("unsupported container runtime: %s", config.ContainerRuntime)
	}

	return service, nil
}

// ProcessExistingContainers 处理现有容器
func (s *IOPSLimitService) ProcessExistingContainers() error {
	containers, err := s.runtime.GetContainers()
	if err != nil {
		return fmt.Errorf("failed to get containers: %v", err)
	}

	for _, container := range containers {
		if err := s.runtime.ProcessContainer(container); err != nil {
			log.Printf("Failed to process container %s: %v", container.ID, err)
		}
	}

	return nil
}

// WatchEvents 监听事件
func (s *IOPSLimitService) WatchEvents() error {
	return s.runtime.WatchContainerEvents()
}

// Close 关闭服务
func (s *IOPSLimitService) Close() error {
	if s.runtime != nil {
		return s.runtime.Close()
	}
	return nil
}

// Run 运行服务
func (s *IOPSLimitService) Run() error {
	log.Println("Starting IOPS limit service...")

	// 确保在服务结束时关闭运行时连接
	defer func() {
		if err := s.Close(); err != nil {
			log.Printf("Error closing runtime connection: %v", err)
		}
	}()

	// 处理现有容器
	if err := s.ProcessExistingContainers(); err != nil {
		log.Printf("Failed to process existing containers: %v", err)
	}

	// 监听新容器事件
	return s.WatchEvents()
}
