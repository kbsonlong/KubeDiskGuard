package main

import (
	"testing"
	"time"

	"iops-limit-service/pkg/config"
	"iops-limit-service/pkg/container"
	"iops-limit-service/pkg/detector"
	"iops-limit-service/pkg/service"
)

func TestDetectRuntime(t *testing.T) {
	runtime := detector.DetectRuntime()
	if runtime == "" {
		t.Error("detectRuntime should not return empty string")
	}
	t.Logf("Detected runtime: %s", runtime)
}

func TestDetectCgroupVersion(t *testing.T) {
	version := detector.DetectCgroupVersion()
	if version != "v1" && version != "v2" {
		t.Errorf("detectCgroupVersion should return v1 or v2, got: %s", version)
	}
	t.Logf("Detected cgroup version: %s", version)
}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name          string
		image         string
		containerName string
		keywords      []string
		expected      bool
	}{
		{
			name:          "should skip pause container",
			image:         "k8s.gcr.io/pause:3.2",
			containerName: "k8s_POD_test-pod",
			keywords:      []string{"pause"},
			expected:      true,
		},
		{
			name:          "should not skip business container",
			image:         "nginx:latest",
			containerName: "nginx-container",
			keywords:      []string{"pause", "istio-proxy"},
			expected:      false,
		},
		{
			name:          "should skip istio-proxy",
			image:         "docker.io/istio/proxyv2:1.12.0",
			containerName: "istio-proxy",
			keywords:      []string{"pause", "istio-proxy"},
			expected:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containerInfo := &container.ContainerInfo{
				Image: tt.image,
				Name:  tt.containerName,
			}
			result := container.ShouldSkip(containerInfo, tt.keywords)
			if result != tt.expected {
				t.Errorf("shouldSkip() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetDefaultConfig(t *testing.T) {
	cfg := config.GetDefaultConfig()
	if cfg == nil {
		t.Fatal("getDefaultConfig should not return nil")
	}

	if cfg.ContainerIOPSLimit != 500 {
		t.Errorf("Expected ContainerIOPSLimit to be 500, got %d", cfg.ContainerIOPSLimit)
	}

	if cfg.DataMount != "/data" {
		t.Errorf("Expected DataMount to be /data, got %s", cfg.DataMount)
	}

	if len(cfg.ExcludeKeywords) == 0 {
		t.Error("Expected ExcludeKeywords to have some default values")
	}
}

func TestConfigToJSON(t *testing.T) {
	cfg := config.GetDefaultConfig()
	jsonStr := cfg.ToJSON()
	if jsonStr == "" {
		t.Error("ToJSON should not return empty string")
	}
	t.Logf("Config JSON: %s", jsonStr)
}

func BenchmarkShouldSkip(b *testing.B) {
	containerInfo := &container.ContainerInfo{
		Image: "nginx:latest",
		Name:  "nginx-container",
	}
	keywords := []string{"pause", "istio-proxy", "psmdb", "kube-system"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		container.ShouldSkip(containerInfo, keywords)
	}
}

func TestEventListening(t *testing.T) {
	// 获取测试配置
	cfg := config.GetDefaultConfig()
	cfg.ContainerRuntime = "docker" // 使用docker进行测试
	cfg.ContainerIOPSLimit = 500
	cfg.DataMount = "/data"
	cfg.ExcludeKeywords = []string{"pause", "istio-proxy"}

	// 创建服务
	svc, err := service.NewIOPSLimitService(cfg)
	if err != nil {
		t.Skipf("Skipping test: failed to create service: %v", err)
	}

	// 测试处理现有容器
	err = svc.ProcessExistingContainers()
	if err != nil {
		t.Logf("Warning: failed to process existing containers: %v", err)
	}

	// 测试事件监听（只运行很短时间）
	done := make(chan bool)
	go func() {
		defer close(done)
		// 只监听5秒钟
		time.Sleep(5 * time.Second)
	}()

	// 启动事件监听
	go func() {
		if err := svc.WatchEvents(); err != nil {
			t.Logf("Event watching stopped: %v", err)
		}
	}()

	// 等待测试完成
	<-done

	// 关闭服务
	if err := svc.Close(); err != nil {
		t.Logf("Warning: failed to close service: %v", err)
	}

	t.Log("Event listening test completed")
}
