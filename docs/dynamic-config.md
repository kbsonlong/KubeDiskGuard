# 动态配置加载功能

## 概述

KubeDiskGuard 现在支持动态配置加载功能，允许在运行时修改配置文件并自动重新加载，无需重启应用程序。

## 功能特性

- ✅ **文件监听**: 自动监听配置文件变化
- ✅ **热重载**: 配置变化时自动重新加载，无需重启
- ✅ **多格式支持**: 支持 JSON 和 YAML 格式配置文件
- ✅ **环境变量优先**: 环境变量配置优先级高于文件配置
- ✅ **回调机制**: 支持配置更新回调函数
- ✅ **线程安全**: 配置读取和更新操作线程安全
- ✅ **错误处理**: 配置文件解析错误时保持原有配置

## 使用方法

### 1. 准备配置文件

复制示例配置文件：
```bash
cp config.example.yaml config.yaml
```

### 2. 设置环境变量

设置配置文件路径：
```bash
export CONFIG_FILE_PATH="/path/to/your/config.yaml"
```

### 3. 启动应用程序

```bash
go run main.go
```

### 4. 动态修改配置

修改 `config.yaml` 文件中的任意配置项，应用程序将在 5 秒内自动检测并重新加载配置。

## 配置文件格式

### YAML 格式示例

```yaml
# 基础配置
monitorInterval: "30s"
diskUsageThreshold: 0.85
logLevel: "info"
enableMetrics: true
metricsPort: 8080

# IOPS 限制配置
defaultReadIOPS: 1000
defaultWriteIOPS: 1000

# 智能速率限制配置
windowConfig:
  enabled: true
  windowSize: "5m"
  maxRequestsPerWindow: 100
```

### JSON 格式示例

```json
{
  "monitorInterval": "30s",
  "diskUsageThreshold": 0.85,
  "logLevel": "info",
  "enableMetrics": true,
  "metricsPort": 8080,
  "defaultReadIOPS": 1000,
  "defaultWriteIOPS": 1000,
  "windowConfig": {
    "enabled": true,
    "windowSize": "5m",
    "maxRequestsPerWindow": 100
  }
}
```

## 配置优先级

配置加载的优先级顺序（从高到低）：

1. **环境变量** - 最高优先级
2. **配置文件** - 中等优先级
3. **默认值** - 最低优先级

## 监听机制

### 轮询方式

当前实现使用轮询方式监听文件变化：
- 检查间隔：5 秒
- 检查方式：文件修改时间比较
- 优点：简单可靠，跨平台兼容
- 缺点：有轻微延迟

### 配置更新流程

1. 检测到文件变化
2. 重新解析配置文件
3. 应用环境变量覆盖
4. 更新内存中的配置
5. 执行配置更新回调
6. 记录配置变化日志

## API 使用示例

### 创建配置监听器

```go
// 创建配置监听器
configWatcher := config.NewConfigWatcher("/path/to/config.yaml", initialConfig)

// 添加配置更新回调
configWatcher.AddUpdateCallback(func(newCfg *config.Config) {
    log.Printf("配置已更新: %s", newCfg.ToJSON())
    // 处理配置更新逻辑
})

// 启动监听
if err := configWatcher.Start(); err != nil {
    log.Printf("配置监听启动失败: %v", err)
}
defer configWatcher.Stop()

// 获取当前配置（线程安全）
currentConfig := configWatcher.GetConfig()
```

### 保存配置到文件

```go
// 修改配置
config := configWatcher.GetConfig()
config.MonitorInterval = time.Minute * 2

// 保存到文件
if err := configWatcher.SaveConfigToFile(config); err != nil {
    log.Printf("保存配置失败: %v", err)
}
```

## 日志输出

配置监听器会输出详细的日志信息：

```
[INFO] 配置文件监听已启动: /path/to/config.yaml
[INFO] 检测到配置文件变化，重新加载配置
[INFO] 配置已更新: {"monitorInterval":"60s",...}
[INFO] 配置变化检测完成，旧配置哈希: a1b2c3d4，新配置哈希: e5f6g7h8
```

## 错误处理

### 常见错误及处理

1. **配置文件不存在**
   - 行为：跳过文件监听，使用默认配置
   - 日志：`[INFO] 配置文件不存在: /path/to/config.yaml，跳过文件监听`

2. **配置文件格式错误**
   - 行为：保持原有配置不变
   - 日志：`[ERROR] 重新加载配置失败: 解析YAML配置文件失败`

3. **文件权限问题**
   - 行为：记录警告，继续使用原有配置
   - 日志：`[WARN] 检查配置文件状态失败: permission denied`

## 性能考虑

- **内存使用**: 配置对象较小，内存占用可忽略
- **CPU 使用**: 轮询检查每 5 秒执行一次，CPU 占用极低
- **I/O 影响**: 仅在文件变化时读取，对磁盘 I/O 影响最小
- **并发安全**: 使用读写锁保证并发安全，性能影响微乎其微

## 最佳实践

1. **配置文件位置**: 建议将配置文件放在应用程序目录或 `/etc` 目录下
2. **权限设置**: 确保应用程序对配置文件有读取权限
3. **备份配置**: 修改配置前建议备份原始配置文件
4. **渐进式更新**: 大幅度配置变更建议分步进行
5. **监控日志**: 关注配置更新相关的日志输出

## 故障排除

### 配置未生效

1. 检查环境变量 `CONFIG_FILE_PATH` 是否正确设置
2. 确认配置文件路径和权限
3. 查看应用程序日志中的配置相关信息
4. 验证配置文件格式是否正确

### 监听不工作

1. 确认文件系统支持文件修改时间
2. 检查应用程序是否有足够权限访问配置文件
3. 验证配置文件路径是否为绝对路径

## 未来改进

- [ ] 支持基于 inotify/fsnotify 的实时文件监听
- [ ] 支持配置文件热验证
- [ ] 支持配置回滚功能
- [ ] 支持远程配置中心集成
- [ ] 支持配置变更审计日志