# 分级智能限速指南

## 概述

分级智能限速功能允许您为不同的时间窗口设置不同的阈值和限速值，实现更精细的IO控制策略。系统支持自动解除限速，确保IO恢复正常后及时释放资源。

## 功能特性

### 1. 时间窗口分级
- **15分钟窗口**：短期高IO检测，快速响应
- **30分钟窗口**：中期高IO检测，中等限速
- **60分钟窗口**：长期高IO检测，轻度限速

### 2. 优先级机制
系统按以下优先级检查：
1. 15分钟窗口（最高优先级）
2. 30分钟窗口（中等优先级）
3. 60分钟窗口（最低优先级）

一旦某个时间窗口触发限速，就不再检查后续窗口。

### 3. 动态限速值
每个时间窗口可以设置独立的：
- IOPS阈值
- BPS阈值
- IOPS限速值
- BPS限速值

### 4. 自动解除限速
- **延迟解除**：限速后等待一定时间再检查解除条件
- **阈值检查**：IO降低到安全阈值以下时自动解除
- **状态跟踪**：完整记录限速和解除过程

## 配置说明

### 启用分级限速

```bash
# 启用分级限速功能
export SMART_LIMIT_GRADED_THRESHOLDS=true
```

### 15分钟窗口配置

```bash
# 15分钟窗口阈值（相对值，0.6表示60%）
export SMART_LIMIT_IO_THRESHOLD_15M=0.6
export SMART_LIMIT_BPS_THRESHOLD_15M=0.6

# 15分钟窗口限速值（绝对值）
export SMART_LIMIT_IOPS_LIMIT_15M=300
export SMART_LIMIT_BPS_LIMIT_15M=50000000
```

### 30分钟窗口配置

```bash
# 30分钟窗口阈值
export SMART_LIMIT_IO_THRESHOLD_30M=0.7
export SMART_LIMIT_BPS_THRESHOLD_30M=0.7

# 30分钟窗口限速值
export SMART_LIMIT_IOPS_LIMIT_30M=400
export SMART_LIMIT_BPS_LIMIT_30M=60000000
```

### 60分钟窗口配置

```bash
# 60分钟窗口阈值
export SMART_LIMIT_IO_THRESHOLD_60M=0.8
export SMART_LIMIT_BPS_THRESHOLD_60M=0.8

# 60分钟窗口限速值
export SMART_LIMIT_IOPS_LIMIT_60M=450
export SMART_LIMIT_BPS_LIMIT_60M=70000000
```

### 解除限速配置

```bash
# 解除限速阈值（相对值，0.5表示50%）
export SMART_LIMIT_REMOVE_THRESHOLD=0.5

# 解除限速延迟（分钟）
export SMART_LIMIT_REMOVE_DELAY=5

# 解除限速检查间隔（分钟）
export SMART_LIMIT_REMOVE_CHECK_INTERVAL=1
```

## 限速生命周期

### 1. 触发限速
- 检测到IO超过阈值
- 应用对应时间窗口的限速值
- 记录限速状态和时间

### 2. 限速维持
- 持续监控IO状态
- 定期检查解除条件
- 更新检查时间戳

### 3. 解除限速
- **延迟检查**：等待 `SMART_LIMIT_REMOVE_DELAY` 分钟
- **阈值检查**：IO降低到 `SMART_LIMIT_REMOVE_THRESHOLD` 以下
- **间隔检查**：每 `SMART_LIMIT_REMOVE_CHECK_INTERVAL` 分钟检查一次
- **自动解除**：满足条件时自动移除限速注解

## 使用场景

### 场景1：突发IO处理
- **15分钟窗口**：阈值0.6，限速300 IOPS
- **用途**：快速响应突发IO，防止系统过载
- **解除**：5分钟后检查，IO<50%时解除

### 场景2：持续高IO处理
- **30分钟窗口**：阈值0.7，限速400 IOPS
- **用途**：处理持续的高IO负载
- **解除**：5分钟后检查，IO<50%时解除

### 场景3：长期IO优化
- **60分钟窗口**：阈值0.8，限速450 IOPS
- **用途**：对长期高IO进行轻度限制
- **解除**：5分钟后检查，IO<50%时解除

## 配置示例

### 推荐配置

```json
{
  "smart_limit_enabled": true,
  "smart_limit_graded_thresholds": true,
  "smart_limit_monitor_interval": 60,
  "smart_limit_history_window": 120,
  
  "smart_limit_io_threshold_15m": 0.6,
  "smart_limit_bps_threshold_15m": 0.6,
  "smart_limit_iops_limit_15m": 300,
  "smart_limit_bps_limit_15m": 50000000,
  
  "smart_limit_io_threshold_30m": 0.7,
  "smart_limit_bps_threshold_30m": 0.7,
  "smart_limit_iops_limit_30m": 400,
  "smart_limit_bps_limit_30m": 60000000,
  
  "smart_limit_io_threshold_60m": 0.8,
  "smart_limit_bps_threshold_60m": 0.8,
  "smart_limit_iops_limit_60m": 450,
  "smart_limit_bps_limit_60m": 70000000,
  
  "smart_limit_remove_threshold": 0.5,
  "smart_limit_remove_delay": 5,
  "smart_limit_remove_check_interval": 1
}
```

### 激进配置（快速响应）

```json
{
  "smart_limit_io_threshold_15m": 0.5,
  "smart_limit_iops_limit_15m": 200,
  "smart_limit_io_threshold_30m": 0.6,
  "smart_limit_iops_limit_30m": 300,
  "smart_limit_io_threshold_60m": 0.7,
  "smart_limit_iops_limit_60m": 400,
  "smart_limit_remove_threshold": 0.4,
  "smart_limit_remove_delay": 3,
  "smart_limit_remove_check_interval": 1
}
```

### 保守配置（宽松限制）

```json
{
  "smart_limit_io_threshold_15m": 0.8,
  "smart_limit_iops_limit_15m": 500,
  "smart_limit_io_threshold_30m": 0.85,
  "smart_limit_iops_limit_30m": 600,
  "smart_limit_io_threshold_60m": 0.9,
  "smart_limit_iops_limit_60m": 700,
  "smart_limit_remove_threshold": 0.6,
  "smart_limit_remove_delay": 10,
  "smart_limit_remove_check_interval": 2
}
```

## 监控和日志

### Pod注解信息

#### 限速时添加的注解

```yaml
annotations:
  io-limit/triggered-by: "15m"                    # 触发窗口
  io-limit/trigger-reason: "15m窗口触发[ReadIOPS:0.65,WriteIOPS:0.58]"  # 触发原因
  io-limit/read-iops-limit: "300"                 # 读IOPS限速值
  io-limit/write-iops-limit: "300"                # 写IOPS限速值
  io-limit/read-bps-limit: "50000000"             # 读BPS限速值
  io-limit/write-bps-limit: "50000000"            # 写BPS限速值
  io-limit/trend-read-iops-15m: "0.65"            # 15分钟读IOPS趋势
  io-limit/trend-write-iops-15m: "0.58"           # 15分钟写IOPS趋势
  # ... 其他时间窗口的趋势数据
```

#### 解除限速时添加的注解

```yaml
annotations:
  io-limit/limit-removed: "true"                  # 限速已解除标记
  io-limit/removed-at: "2024-01-15T10:30:00Z"     # 解除时间
  io-limit/removed-reason: "IO已恢复正常[ReadIOPS:0.45,WriteIOPS:0.42], 阈值:0.50"  # 解除原因
```

### 日志输出

#### 限速日志
```
Applied graded smart limit to pod default/myapp: 15m窗口触发[ReadIOPS:0.65,WriteIOPS:0.58], IOPS[300,300], BPS[50000000,50000000]
```

#### 解除限速日志
```
Removed smart limit from pod default/myapp: IO已恢复正常[ReadIOPS:0.45,WriteIOPS:0.42], 阈值:0.50
```

## 最佳实践

### 1. 阈值设置原则
- **15分钟窗口**：设置较低阈值，快速响应
- **30分钟窗口**：设置中等阈值，平衡性能
- **60分钟窗口**：设置较高阈值，避免误判

### 2. 限速值设置原则
- **15分钟窗口**：设置较严格的限速值
- **30分钟窗口**：设置中等限速值
- **60分钟窗口**：设置较宽松的限速值

### 3. 解除限速设置原则
- **解除阈值**：通常设置为触发阈值的70-80%
- **解除延迟**：5-10分钟，避免频繁切换
- **检查间隔**：1-2分钟，平衡响应速度和性能

### 4. 监控建议
- 定期检查Pod注解中的趋势数据
- 根据实际IO模式调整阈值
- 监控系统整体IO性能
- 关注限速和解除的频率

### 5. 故障排除
- 检查 `SMART_LIMIT_GRADED_THRESHOLDS` 是否启用
- 验证各时间窗口的阈值和限速值配置
- 查看Pod注解中的触发原因和解除原因
- 确认解除限速的配置参数

## 兼容性

- 当 `SMART_LIMIT_GRADED_THRESHOLDS=false` 时，使用原有的单一阈值逻辑
- 支持平滑升级，不会影响现有配置
- 向后兼容原有的 `SmartLimitHighIOThreshold` 和 `SmartLimitHighBPSThreshold` 配置
- 解除限速功能对所有模式都有效 