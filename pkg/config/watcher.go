package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/yaml.v2"
)

// ConfigWatcher 配置文件监听器
type ConfigWatcher struct {
	mu           sync.RWMutex
	configPath   string
	config       *Config
	stopCh       chan struct{}
	updateCallbacks []func(*Config)
	lastModTime  time.Time
}

// NewConfigWatcher 创建配置文件监听器
func NewConfigWatcher(configPath string, initialConfig *Config) *ConfigWatcher {
	return &ConfigWatcher{
		configPath:      configPath,
		config:         initialConfig,
		stopCh:         make(chan struct{}),
		updateCallbacks: make([]func(*Config), 0),
	}
}

// AddUpdateCallback 添加配置更新回调函数
func (w *ConfigWatcher) AddUpdateCallback(callback func(*Config)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.updateCallbacks = append(w.updateCallbacks, callback)
}

// GetConfig 获取当前配置（线程安全）
func (w *ConfigWatcher) GetConfig() *Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config
}

// Start 启动配置文件监听
func (w *ConfigWatcher) Start() error {
	if w.configPath == "" {
		log.Printf("[INFO] 配置文件路径为空，跳过文件监听")
		return nil
	}

	// 检查配置文件是否存在
	if _, err := os.Stat(w.configPath); os.IsNotExist(err) {
		log.Printf("[INFO] 配置文件不存在: %s，跳过文件监听", w.configPath)
		return nil
	}

	// 获取初始文件修改时间
	if err := w.updateLastModTime(); err != nil {
		return fmt.Errorf("获取配置文件修改时间失败: %v", err)
	}

	// 尝试加载初始配置
	if err := w.loadConfigFromFile(); err != nil {
		log.Printf("[WARN] 初始配置文件加载失败: %v，使用默认配置", err)
	}

	go w.watchLoop()
	log.Printf("[INFO] 配置文件监听已启动: %s", w.configPath)
	return nil
}

// Stop 停止配置文件监听
func (w *ConfigWatcher) Stop() {
	close(w.stopCh)
	log.Printf("[INFO] 配置文件监听已停止")
}

// watchLoop 监听循环（轮询方式）
func (w *ConfigWatcher) watchLoop() {
	ticker := time.NewTicker(5 * time.Second) // 每5秒检查一次
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.checkForUpdates()
		}
	}
}

// checkForUpdates 检查配置文件是否有更新
func (w *ConfigWatcher) checkForUpdates() {
	stat, err := os.Stat(w.configPath)
	if err != nil {
		log.Printf("[WARN] 检查配置文件状态失败: %v", err)
		return
	}

	w.mu.RLock()
	lastModTime := w.lastModTime
	w.mu.RUnlock()

	if stat.ModTime().After(lastModTime) {
		log.Printf("[INFO] 检测到配置文件变化，重新加载配置")
		if err := w.reloadConfig(); err != nil {
			log.Printf("[ERROR] 重新加载配置失败: %v", err)
		} else {
			w.mu.Lock()
			w.lastModTime = stat.ModTime()
			w.mu.Unlock()
		}
	}
}

// updateLastModTime 更新最后修改时间
func (w *ConfigWatcher) updateLastModTime() error {
	stat, err := os.Stat(w.configPath)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.lastModTime = stat.ModTime()
	w.mu.Unlock()
	return nil
}

// reloadConfig 重新加载配置
func (w *ConfigWatcher) reloadConfig() error {
	newConfig, err := w.loadConfigFromFileInternal()
	if err != nil {
		return err
	}

	// 从环境变量覆盖配置（保持环境变量优先级）
	LoadFromEnv(newConfig)

	w.mu.Lock()
	oldConfig := w.config
	w.config = newConfig
	callbacks := make([]func(*Config), len(w.updateCallbacks))
	copy(callbacks, w.updateCallbacks)
	w.mu.Unlock()

	// 执行回调函数
	for _, callback := range callbacks {
		go func(cb func(*Config)) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[ERROR] 配置更新回调执行失败: %v", r)
				}
			}()
			cb(newConfig)
		}(callback)
	}

	log.Printf("[INFO] 配置已更新: %s", newConfig.ToJSON())
	log.Printf("[INFO] 配置变化检测完成，旧配置哈希: %x，新配置哈希: %x", 
		w.configHash(oldConfig), w.configHash(newConfig))

	return nil
}

// loadConfigFromFile 从文件加载配置（公开方法）
func (w *ConfigWatcher) loadConfigFromFile() error {
	newConfig, err := w.loadConfigFromFileInternal()
	if err != nil {
		return err
	}

	w.mu.Lock()
	w.config = newConfig
	w.mu.Unlock()

	return nil
}

// loadConfigFromFileInternal 从文件加载配置（内部方法）
func (w *ConfigWatcher) loadConfigFromFileInternal() (*Config, error) {
	data, err := ioutil.ReadFile(w.configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 创建默认配置作为基础
	newConfig := GetDefaultConfig()

	// 根据文件扩展名选择解析方式
	ext := filepath.Ext(w.configPath)
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, newConfig); err != nil {
			return nil, fmt.Errorf("解析JSON配置文件失败: %v", err)
		}
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, newConfig); err != nil {
			return nil, fmt.Errorf("解析YAML配置文件失败: %v", err)
		}
	default:
		return nil, fmt.Errorf("不支持的配置文件格式: %s", ext)
	}

	return newConfig, nil
}

// configHash 计算配置的简单哈希值（用于变化检测）
func (w *ConfigWatcher) configHash(cfg *Config) uint32 {
	data, _ := json.Marshal(cfg)
	hash := uint32(0)
	for _, b := range data {
		hash = hash*31 + uint32(b)
	}
	return hash
}

// SaveConfigToFile 保存配置到文件
func (w *ConfigWatcher) SaveConfigToFile(cfg *Config) error {
	if w.configPath == "" {
		return fmt.Errorf("配置文件路径为空")
	}

	// 确保目录存在
	dir := filepath.Dir(w.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %v", err)
	}

	// 根据文件扩展名选择保存格式
	ext := filepath.Ext(w.configPath)
	var data []byte
	var err error

	switch ext {
	case ".json":
		data, err = json.MarshalIndent(cfg, "", "  ")
	case ".yaml", ".yml":
		data, err = yaml.Marshal(cfg)
	default:
		return fmt.Errorf("不支持的配置文件格式: %s", ext)
	}

	if err != nil {
		return fmt.Errorf("序列化配置失败: %v", err)
	}

	// 写入临时文件，然后原子性替换
	tempPath := w.configPath + ".tmp"
	if err := ioutil.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("写入临时配置文件失败: %v", err)
	}

	if err := os.Rename(tempPath, w.configPath); err != nil {
		os.Remove(tempPath) // 清理临时文件
		return fmt.Errorf("替换配置文件失败: %v", err)
	}

	log.Printf("[INFO] 配置已保存到文件: %s", w.configPath)
	return nil
}