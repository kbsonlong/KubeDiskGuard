# KubeDiskGuard 热重载功能指南

## 概述

KubeDiskGuard 支持配置热重载功能，允许在不重启服务的情况下动态更新配置。当配置文件发生变化时，服务会自动检测并应用新的配置。

## 功能特性

### 支持热重载的配置项

- **IOPS 限制配置**
  - `container_iops_limit`: 容器总IOPS限制
  - `container_read_iops_limit`: 容器读IOPS限制
  - `container_write_iops_limit`: 容器写IOPS限制

- **BPS 限制配置**
  - `container_bps_limit`: 容器总BPS限制
  - `container_read_bps_limit`: 容器读BPS限制
  - `container_write_bps_limit`: 容器写BPS限制

- **智能限速配置**
  - `smart_limit_enabled`: 智能限速开关
  - `smart_limit_monitor_interval`: 监控间隔
  - `smart_limit_annotation_prefix`: 注解前缀
  - `smart_limit_windows`: 监控窗口配置

- **排除规则配置**
  - `exclude_keywords`: 排除关键词
  - `exclude_namespaces`: 排除命名空间
  - `exclude_label_selector`: 排除标签选择器

## 使用方法

### 1. 启用配置文件监听

设置环境变量 `CONFIG_FILE_PATH` 指向配置文件：

```bash
export CONFIG_FILE_PATH="/path/to/config.json"
```

### 2. 启动服务

```bash
./iops-limit-service
```

### 3. 动态更新配置

修改配置文件后，服务会自动检测变化并应用新配置：

```bash
# 编辑配置文件
vim /path/to/config.json

# 服务会自动检测并应用新配置
```

## 热重载流程

### 1. 配置检测
- 服务每5秒检查一次配置文件
- 通过文件修改时间判断是否发生变化

### 2. 配置验证
- 解析新的配置文件
- 验证配置格式和有效性
- 合并环境变量配置（环境变量优先级更高）

### 3. 服务更新
- 更新服务内部配置引用
- 重新处理现有容器以应用新配置
- 更新智能限速管理器配置

### 4. 组件重启
- 如果监控间隔发生变化，重启监控循环
- 如果智能限速开关状态变化，启动或停止智能限速管理器

## 配置示例

### 初始配置

```json
{
    "container_iops_limit": 1000,
    "container_read_iops_limit": 500,
    "container_write_iops_limit": 500,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 30
}
```

### 更新配置（增加IOPS限制）

```json
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 30
}
```

### 更新配置（禁用智能限速）

```json
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "smart_limit_enabled": false,
    "smart_limit_monitor_interval": 30
}
```

## 监控和日志

### 日志信息

热重载过程中会输出详细的日志信息：

```
[INFO] 检测到配置文件变化，重新加载配置
[INFO] 配置已更新: {"container_iops_limit":2000,...}
[INFO] 检测到配置更新，开始热重载服务...
[INFO] 开始热重载配置...
[INFO] 智能限速管理器配置已更新
[INFO] 现有容器已重新处理完成
[INFO] 配置热重载完成
[INFO] 服务热重载成功完成
```

### 监控指标

可以通过 Prometheus 指标监控热重载状态：

```bash
curl http://localhost:2112/metrics
```

## 测试热重载功能

使用提供的测试脚本验证热重载功能：

```bash
# 设置必要的环境变量
export NODE_NAME="your-node-name"

# 运行测试
./scripts/test-hot-reload.sh
```

测试脚本会验证以下场景：
1. IOPS限制更新
2. 监控间隔更新
3. 智能限速启用/禁用
4. 配置热重载日志

## 注意事项

### 1. 配置文件格式
- 支持 JSON 和 YAML 格式
- 确保配置文件语法正确
- 建议使用配置文件验证工具

### 2. 权限要求
- 确保服务有读取配置文件的权限
- 配置文件路径必须正确设置

### 3. 性能影响
- 热重载会重新处理所有现有容器
- 大量容器时可能需要较长时间
- 建议在低峰期进行配置更新

### 4. 错误处理
- 配置文件解析失败时，会保持原有配置
- 热重载失败时，会记录错误日志
- 服务不会因为配置错误而停止

### 5. 兼容性
- 新增配置项会使用默认值
- 删除配置项会保持原有值
- 建议逐步更新配置，避免大幅变更

## 故障排除

### 常见问题

1. **配置文件未检测到变化**
   - 检查 `CONFIG_FILE_PATH` 环境变量
   - 确认配置文件路径正确
   - 检查文件权限

2. **热重载失败**
   - 检查配置文件格式
   - 查看服务日志
   - 确认配置项名称正确

3. **智能限速未更新**
   - 检查 `smart_limit_enabled` 配置
   - 确认监控间隔设置
   - 查看智能限速日志

### 调试方法

1. **启用详细日志**
   ```bash
   export LOG_LEVEL=debug
   ```

2. **检查配置文件**
   ```bash
   # 验证JSON格式
   jq . /path/to/config.json
   
   # 验证YAML格式
   yamllint /path/to/config.yaml
   ```

3. **监控文件变化**
   ```bash
   # 监控配置文件变化
   tail -f /path/to/config.json
   
   # 监控服务日志
   tail -f /var/log/kubediskguard.log
   ```

## 最佳实践

1. **配置文件管理**
   - 使用版本控制管理配置文件
   - 备份重要配置
   - 使用配置模板

2. **更新策略**
   - 在测试环境验证配置
   - 逐步更新生产配置
   - 监控服务状态

3. **监控告警**
   - 设置配置更新告警
   - 监控热重载成功率
   - 跟踪配置变更历史

4. **安全考虑**
   - 限制配置文件访问权限
   - 使用配置文件加密
   - 定期审查配置变更 