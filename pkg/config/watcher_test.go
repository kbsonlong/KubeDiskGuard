package config

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

func TestConfigWatcher_NewConfigWatcher(t *testing.T) {
	initialConfig := GetDefaultConfig()
	watcher := NewConfigWatcher("/test/path", initialConfig)

	assert.NotNil(t, watcher)
	assert.Equal(t, "/test/path", watcher.configPath)
	assert.Equal(t, initialConfig, watcher.GetConfig())
	assert.NotNil(t, watcher.stopCh)
	assert.Empty(t, watcher.updateCallbacks)
}

func TestConfigWatcher_AddUpdateCallback(t *testing.T) {
	watcher := NewConfigWatcher("", GetDefaultConfig())
	callbackCalled := false

	watcher.AddUpdateCallback(func(*Config) {
		callbackCalled = true
	})

	assert.Len(t, watcher.updateCallbacks, 1)

	// 测试回调函数
	watcher.updateCallbacks[0](nil)
	assert.True(t, callbackCalled)
}

func TestConfigWatcher_GetConfig_ThreadSafe(t *testing.T) {
	initialConfig := GetDefaultConfig()
	watcher := NewConfigWatcher("", initialConfig)

	// 并发读取配置
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			config := watcher.GetConfig()
			assert.NotNil(t, config)
		}()
	}
	wg.Wait()
}

func TestConfigWatcher_LoadConfigFromFile_JSON(t *testing.T) {
	// 创建临时JSON配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	testConfig := map[string]interface{}{
		"container_iops_limit":      1000,
		"container_read_iops_limit": 800,
		"data_mount":               "/test",
		"smart_limit_enabled":      true,
	}

	configData, err := json.MarshalIndent(testConfig, "", "  ")
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	// 测试加载配置
	watcher := NewConfigWatcher(configPath, GetDefaultConfig())
	err = watcher.loadConfigFromFile()
	require.NoError(t, err)

	config := watcher.GetConfig()
	assert.Equal(t, 1000, config.ContainerIOPSLimit)
	assert.Equal(t, 800, config.ContainerReadIOPSLimit)
	assert.Equal(t, "/test", config.DataMount)
	assert.True(t, config.SmartLimitEnabled)
}

func TestConfigWatcher_LoadConfigFromFile_YAML(t *testing.T) {
	// 创建临时YAML配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	testConfig := map[string]interface{}{
		"container_write_iops_limit": 1200,
		"container_read_bps_limit":   1048576,
		"kubelet_port":              "10251",
		"smart_limit_monitor_interval": 120,
	}

	configData, err := yaml.Marshal(testConfig)
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	// 测试加载配置
	watcher := NewConfigWatcher(configPath, GetDefaultConfig())
	err = watcher.loadConfigFromFile()
	require.NoError(t, err)

	config := watcher.GetConfig()
	assert.Equal(t, 1200, config.ContainerWriteIOPSLimit)
	assert.Equal(t, 1048576, config.ContainerReadBPSLimit)
	assert.Equal(t, "10251", config.KubeletPort)
	assert.Equal(t, 120, config.SmartLimitMonitorInterval)
}

func TestConfigWatcher_LoadConfigFromFile_UnsupportedFormat(t *testing.T) {
	// 创建不支持格式的配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.txt")

	err := ioutil.WriteFile(configPath, []byte("invalid config"), 0644)
	require.NoError(t, err)

	watcher := NewConfigWatcher(configPath, GetDefaultConfig())
	err = watcher.loadConfigFromFile()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "不支持的配置文件格式")
}

func TestConfigWatcher_SaveConfigToFile_JSON(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	watcher := NewConfigWatcher(configPath, GetDefaultConfig())
	config := watcher.GetConfig()
	config.ContainerIOPSLimit = 2000
	config.SmartLimitEnabled = true

	err := watcher.SaveConfigToFile(config)
	require.NoError(t, err)

	// 验证文件内容
	data, err := ioutil.ReadFile(configPath)
	require.NoError(t, err)

	var savedConfig Config
	err = json.Unmarshal(data, &savedConfig)
	require.NoError(t, err)

	assert.Equal(t, 2000, savedConfig.ContainerIOPSLimit)
	assert.True(t, savedConfig.SmartLimitEnabled)
}

func TestConfigWatcher_SaveConfigToFile_YAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	watcher := NewConfigWatcher(configPath, GetDefaultConfig())
	config := watcher.GetConfig()
	config.KubeletPort = "9999"
	config.DataMount = "/trace"

	err := watcher.SaveConfigToFile(config)
	require.NoError(t, err)

	// 验证文件内容
	data, err := ioutil.ReadFile(configPath)
	require.NoError(t, err)

	var savedConfig Config
	err = yaml.Unmarshal(data, &savedConfig)
	require.NoError(t, err)

	assert.Equal(t, "9999", savedConfig.KubeletPort)
	assert.Equal(t, "/trace", savedConfig.DataMount)
}

func TestConfigWatcher_Start_FileNotExists(t *testing.T) {
	nonExistentPath := "/non/existent/config.yaml"
	watcher := NewConfigWatcher(nonExistentPath, GetDefaultConfig())

	// 应该不返回错误，只是跳过文件监听
	err := watcher.Start()
	assert.NoError(t, err)

	watcher.Stop()
}

func TestConfigWatcher_Start_EmptyPath(t *testing.T) {
	watcher := NewConfigWatcher("", GetDefaultConfig())

	// 应该不返回错误，只是跳过文件监听
	err := watcher.Start()
	assert.NoError(t, err)

	watcher.Stop()
}

func TestConfigWatcher_ReloadConfig_WithCallback(t *testing.T) {
	// 创建临时配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialConfig := map[string]interface{}{
		"container_iops_limit": 500,
		"data_mount":          "/data",
	}

	configData, err := json.MarshalIndent(initialConfig, "", "  ")
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	// 创建监听器并添加回调
	watcher := NewConfigWatcher(configPath, GetDefaultConfig())
	callbackCount := 0
	var callbackConfig *Config

	watcher.AddUpdateCallback(func(newCfg *Config) {
		callbackCount++
		callbackConfig = newCfg
	})

	// 加载初始配置
	err = watcher.loadConfigFromFile()
	require.NoError(t, err)

	// 修改配置文件
	updatedConfig := map[string]interface{}{
		"container_iops_limit": 1000,
		"data_mount":          "/debug",
	}

	updatedData, err := json.MarshalIndent(updatedConfig, "", "  ")
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, updatedData, 0644)
	require.NoError(t, err)

	// 触发重新加载
	err = watcher.reloadConfig()
	require.NoError(t, err)

	// 等待回调执行
	time.Sleep(100 * time.Millisecond)

	// 验证回调被调用
	assert.Equal(t, 1, callbackCount)
	assert.NotNil(t, callbackConfig)
	assert.Equal(t, 1000, callbackConfig.ContainerIOPSLimit)
	assert.Equal(t, "/debug", callbackConfig.DataMount)
}

func TestConfigWatcher_ConfigHash(t *testing.T) {
	watcher := NewConfigWatcher("", GetDefaultConfig())

	config1 := GetDefaultConfig()
	config2 := GetDefaultConfig()
	config3 := GetDefaultConfig()
	config3.ContainerIOPSLimit = 2000

	hash1 := watcher.configHash(config1)
	hash2 := watcher.configHash(config2)
	hash3 := watcher.configHash(config3)

	// 相同配置应该有相同哈希
	assert.Equal(t, hash1, hash2)

	// 不同配置应该有不同哈希
	assert.NotEqual(t, hash1, hash3)
}

func TestConfigWatcher_ToJSON(t *testing.T) {
	config := GetDefaultConfig()
	jsonStr := config.ToJSON()

	assert.NotEmpty(t, jsonStr)
	assert.NotEqual(t, "配置序列化失败", jsonStr)

	// 验证是否为有效JSON
	var result map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	assert.NoError(t, err)
}

func TestConfigWatcher_ConcurrentAccess(t *testing.T) {
	// 创建临时配置文件
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.json")

	initialConfig := map[string]interface{}{
		"container_iops_limit": 500,
		"data_mount":          "/data",
	}

	configData, err := json.MarshalIndent(initialConfig, "", "  ")
	require.NoError(t, err)

	err = ioutil.WriteFile(configPath, configData, 0644)
	require.NoError(t, err)

	watcher := NewConfigWatcher(configPath, GetDefaultConfig())

	// 并发读取和更新配置
	var wg sync.WaitGroup

	// 启动多个读取协程
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				config := watcher.GetConfig()
				assert.NotNil(t, config)
			}
		}()
	}

	// 启动配置更新协程
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				updatedConfig := map[string]interface{}{
					"container_iops_limit": 1000,
					"data_mount":          "/debug",
					"kubelet_port":        strconv.Itoa(10250 + index),
				}

				updatedData, _ := json.MarshalIndent(updatedConfig, "", "  ")
				ioutil.WriteFile(configPath, updatedData, 0644)
				watcher.reloadConfig()
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
}