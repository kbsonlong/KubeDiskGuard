# cAdvisor IO 指标计算详解

## 概述

cAdvisor 提供的磁盘 IO 指标是累积值（counter），需要通过计算增量来得到实时的 IOPS 和 BPS。本文档详细说明如何正确计算这些指标。

## cAdvisor IO 指标

### 主要指标

| 指标名称 | 类型 | 说明 |
|---------|------|------|
| `container_fs_reads_total` | Counter | 累积读取操作次数 |
| `container_fs_writes_total` | Counter | 累积写入操作次数 |
| `container_fs_reads_bytes_total` | Counter | 累积读取字节数 |
| `container_fs_writes_bytes_total` | Counter | 累积写入字节数 |

### 指标格式示例

```
# HELP container_fs_reads_total Cumulative count of reads completed
# TYPE container_fs_reads_total counter
container_fs_reads_total{container="nginx",id="/docker/1234567890abcdef"} 1500

# HELP container_fs_writes_total Cumulative count of writes completed
# TYPE container_fs_writes_total counter
container_fs_writes_total{container="nginx",id="/docker/1234567890abcdef"} 800

# HELP container_fs_reads_bytes_total Cumulative count of bytes read
# TYPE container_fs_reads_bytes_total counter
container_fs_reads_bytes_total{container="nginx",id="/docker/1234567890abcdef"} 104857600

# HELP container_fs_writes_bytes_total Cumulative count of bytes written
# TYPE container_fs_writes_bytes_total counter
container_fs_writes_bytes_total{container="nginx",id="/docker/1234567890abcdef"} 52428800
```

## 计算方法

### 1. IOPS 计算

**IOPS (Input/Output Operations Per Second)** = 每秒输入/输出操作数

```
IOPS = (当前累积操作数 - 上次累积操作数) / 时间间隔(秒)
```

**示例计算：**
```
时间点 T1: container_fs_reads_total = 1000
时间点 T2: container_fs_reads_total = 1100
时间间隔: 10秒

读取 IOPS = (1100 - 1000) / 10 = 10 ops/s
```

### 2. BPS 计算

**BPS (Bytes Per Second)** = 每秒字节数

```
BPS = (当前累积字节数 - 上次累积字节数) / 时间间隔(秒)
```

**示例计算：**
```
时间点 T1: container_fs_reads_bytes_total = 104857600 (100MB)
时间点 T2: container_fs_reads_bytes_total = 115343360 (110MB)
时间间隔: 10秒

读取 BPS = (115343360 - 104857600) / 10 = 1048576 bytes/s = 1MB/s
```

## 实现代码

### 1. 数据点结构

```go
type MetricPoint struct {
    ContainerID string
    Timestamp   time.Time
    ReadIOPS    float64  // 累积读取次数
    WriteIOPS   float64  // 累积写入次数
    ReadBytes   float64  // 累积读取字节数
    WriteBytes  float64  // 累积写入字节数
}
```

### 2. IO 速率结构

```go
type IORate struct {
    ContainerID string
    Timestamp   time.Time
    ReadIOPS    float64  // 每秒读取操作数
    WriteIOPS   float64  // 每秒写入操作数
    ReadBPS     float64  // 每秒读取字节数
    WriteBPS    float64  // 每秒写入字节数
}
```

### 3. 计算逻辑

```go
func CalculateIORate(points []MetricPoint, window time.Duration) (*IORate, error) {
    if len(points) < 2 {
        return nil, fmt.Errorf("insufficient data points")
    }

    // 找到窗口内的数据点
    cutoff := time.Now().Add(-window)
    var windowPoints []MetricPoint
    
    for _, point := range points {
        if point.Timestamp.After(cutoff) {
            windowPoints = append(windowPoints, point)
        }
    }

    if len(windowPoints) < 2 {
        return nil, fmt.Errorf("insufficient data points in window")
    }

    // 计算增量
    latest := windowPoints[len(windowPoints)-1]
    earliest := windowPoints[0]

    timeDiff := latest.Timestamp.Sub(earliest.Timestamp).Seconds()
    if timeDiff <= 0 {
        return nil, fmt.Errorf("invalid time difference")
    }

    // 计算IOPS
    readIOPSDelta := latest.ReadIOPS - earliest.ReadIOPS
    writeIOPSDelta := latest.WriteIOPS - earliest.WriteIOPS
    readIOPS := readIOPSDelta / timeDiff
    writeIOPS := writeIOPSDelta / timeDiff

    // 计算BPS
    readBytesDelta := latest.ReadBytes - earliest.ReadBytes
    writeBytesDelta := latest.WriteBytes - earliest.WriteBytes
    readBPS := readBytesDelta / timeDiff
    writeBPS := writeBytesDelta / timeDiff

    return &IORate{
        ContainerID: latest.ContainerID,
        Timestamp:   latest.Timestamp,
        ReadIOPS:    readIOPS,
        WriteIOPS:   writeIOPS,
        ReadBPS:     readBPS,
        WriteBPS:    writeBPS,
    }, nil
}
```

## 时间窗口策略

### 1. 单窗口计算

适用于实时监控，计算特定时间窗口内的 IO 速率：

```go
// 计算最近1分钟的IO速率
rate, err := calculator.CalculateIORate(containerID, 1*time.Minute)
```

### 2. 多窗口平均

适用于趋势分析，计算多个时间窗口的平均值：

```go
windows := []time.Duration{
    15 * time.Minute,
    30 * time.Minute,
    60 * time.Minute,
}

avgRate, err := calculator.CalculateAverageIORate(containerID, windows)
```

## 实际应用示例

### 1. 数据采集

```go
// 从 cAdvisor 获取指标
metrics, err := kubeletClient.GetCadvisorMetrics()
if err != nil {
    return err
}

// 解析指标
parsedMetrics, err := kubeletClient.ParseCadvisorMetrics(metrics)
if err != nil {
    return err
}

// 为每个容器添加数据点
for containerID, readIOPS := range parsedMetrics.ContainerFSReadsTotal {
    writeIOPS := parsedMetrics.ContainerFSWritesTotal[containerID]
    readBytes := parsedMetrics.ContainerFSReadsBytesTotal[containerID]
    writeBytes := parsedMetrics.ContainerFSWritesBytesTotal[containerID]

    calculator.AddMetricPoint(containerID, time.Now(), 
        readIOPS, writeIOPS, readBytes, writeBytes)
}
```

### 2. 智能限速应用

```go
// 计算15分钟、30分钟、60分钟的平均IO速率
windows := []time.Duration{
    15 * time.Minute,
    30 * time.Minute,
    60 * time.Minute,
}

rate, err := calculator.CalculateAverageIORate(containerID, windows)
if err != nil {
    return err
}

// 检查是否需要限速
if rate.ReadIOPS > threshold || rate.WriteIOPS > threshold ||
   rate.ReadBPS > bpsThreshold || rate.WriteBPS > bpsThreshold {
    // 应用智能限速
    applySmartLimit(containerID, rate)
}
```

## 注意事项

### 1. 数据点要求

- **最少2个数据点**：需要至少2个时间点的数据才能计算增量
- **时间间隔**：数据点之间的时间间隔应该合理（建议10-60秒）
- **数据连续性**：避免数据点之间的时间间隔过大

### 2. 精度考虑

- **浮点数精度**：使用 float64 类型避免精度丢失
- **时间精度**：使用纳秒级时间戳确保精度
- **异常值处理**：处理计数器重置等异常情况

### 3. 性能优化

- **数据清理**：定期清理过期的历史数据
- **内存管理**：限制每个容器的历史数据点数量
- **并发安全**：使用读写锁保护共享数据

### 4. 错误处理

```go
// 处理计数器重置
if latest.ReadIOPS < earliest.ReadIOPS {
    // 计数器可能重置，跳过这次计算
    return nil, fmt.Errorf("counter reset detected")
}

// 处理时间异常
if timeDiff > maxTimeDiff {
    return nil, fmt.Errorf("time difference too large")
}
```

## 测试验证

### 1. 运行测试程序

```bash
# 模拟5分钟的IO数据
go run cmd/test-cadvisor-calculation/main.go \
  --container=test-container \
  --duration=5m \
  --interval=10s
```

### 2. 验证计算准确性

```bash
# 检查计算结果是否合理
# IOPS 应该在合理范围内（通常 0-10000 ops/s）
# BPS 应该在合理范围内（通常 0-1GB/s）
```

## 总结

通过正确计算 cAdvisor 累积指标的增量，我们可以得到准确的 IOPS 和 BPS 数据，为智能限速功能提供可靠的数据基础。关键是要：

1. **理解累积指标的特性**：需要计算增量而不是直接使用绝对值
2. **选择合适的时间窗口**：根据应用场景选择合适的时间窗口
3. **处理异常情况**：正确处理计数器重置、数据缺失等异常
4. **优化性能**：合理管理历史数据，避免内存泄漏

这种计算方法为 KubeDiskGuard 的智能限速功能提供了准确、实时的 IO 使用情况数据。 