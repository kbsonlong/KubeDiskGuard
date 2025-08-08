# 限速状态恢复机制指南

## 概述

当 `iops-limit-service` Pod 重启时，内存中的限速状态会丢失。为了确保限速的连续性，系统实现了状态恢复机制，能够从 Pod 注解中恢复限速状态。

## 问题背景

### 原始问题
- Pod 重启后，内存中的限速状态丢失
- 正在被限速的容器会立即取消限速
- 需要等待 IO 再次触发阈值才会重新限速

### 解决方案
- 在启动时扫描所有 Pod 的注解
- 从注解中恢复限速状态到内存
- 确保限速的连续性和一致性

## 恢复机制

### 1. 启动时恢复
```go
// 在 Start() 方法中调用
go m.restoreLimitStatus()
```

### 2. 恢复流程
1. **扫描 Pod**：获取当前节点上的所有 Pod
2. **检查注解**：查找有限速注解的 Pod
3. **解析状态**：从注解中解析限速信息
4. **恢复内存**：将状态恢复到内存中

### 3. 恢复的数据
- 触发窗口（15m/30m/60m/legacy）
- 限速值（IOPS/BPS）
- 触发原因
- 限速状态（IsLimited = true）

## 注解格式

### 限速注解
```yaml
annotations:
  kubediskguard.io/triggered-by: "15m"                    # 触发窗口
  kubediskguard.io/trigger-reason: "15m窗口触发[ReadIOPS:0.65]"  # 触发原因
  kubediskguard.io/read-iops-limit: "300"                 # 读IOPS限速值
  kubediskguard.io/write-iops-limit: "300"                # 写IOPS限速值
  kubediskguard.io/read-bps-limit: "50000000"             # 读BPS限速值
  kubediskguard.io/write-bps-limit: "50000000"            # 写BPS限速值
```

### 解除限速注解
```yaml
annotations:
  kubediskguard.io/limit-removed: "true"                  # 限速已解除标记
  kubediskguard.io/removed-at: "2024-01-15T10:30:00Z"     # 解除时间
  kubediskguard.io/removed-reason: "IO已恢复正常"          # 解除原因
```

## 恢复逻辑

### 1. 检查条件
```go
// 检查Pod是否有限速注解
if m.hasLimitAnnotations(pod.Annotations) {
    // 排除已解除限速的Pod
    if removed, exists := annotations[prefix+"limit-removed"]; 
       exists && removed == "true" {
        return false
    }
}
```

### 2. 解析数据
```go
// 解析触发窗口
triggeredBy := annotations[prefix+"triggered-by"]

// 解析限速值
readIOPS := m.parseIntAnnotation(annotations[prefix+"read-iops-limit"], 0)
writeIOPS := m.parseIntAnnotation(annotations[prefix+"write-iops-limit"], 0)
readBPS := m.parseIntAnnotation(annotations[prefix+"read-bps-limit"], 0)
writeBPS := m.parseIntAnnotation(annotations[prefix+"write-bps-limit"], 0)
```

### 3. 恢复状态
```go
// 创建限速结果
limitResult := &LimitResult{
    TriggeredBy: triggeredBy,
    ReadIOPS:    readIOPS,
    WriteIOPS:   writeIOPS,
    ReadBPS:     readBPS,
    WriteBPS:    writeBPS,
    Reason:      reason,
}

// 更新限速状态
m.updateLimitStatus(containerID, podName, namespace, true, limitResult)
```

## 状态一致性

### 1. 避免重复限速
- 检查容器是否已经限速
- 只在限速值发生变化时更新
- 避免不必要的 Pod 注解更新

### 2. 限速值更新
```go
// 检查是否需要更新限速值
if m.shouldUpdateLimit(limitStatus, limitResult) {
    // 更新限速值
    m.applySmartLimitWithResult(podName, namespace, trend, limitResult)
    m.updateLimitStatus(containerID, podName, namespace, true, limitResult)
}
```

### 3. 更新条件
- 触发不同的时间窗口
- 限速值发生变化
- 新的触发原因

## 日志输出

### 启动恢复日志
```
Restoring limit status from pod annotations...
Restored limit status for container abc123: 15m窗口触发[ReadIOPS:0.65]
Restored limit status for 2 containers
```

### 状态更新日志
```
Updating limit for container abc123: 30m窗口触发[ReadIOPS:0.75]
```

## 最佳实践

### 1. 监控恢复过程
- 关注启动日志中的恢复信息
- 确认恢复的容器数量
- 检查恢复后的限速状态

### 2. 验证状态一致性
- 检查 Pod 注解是否与内存状态一致
- 确认限速值是否正确恢复
- 验证解除限速的标记

### 3. 故障排除
- 如果恢复失败，检查 Pod 注解格式
- 确认容器 ID 是否正确解析
- 验证限速值是否有效

## 配置建议

### 1. 注解前缀
```bash
# 确保注解前缀一致
export SMART_LIMIT_ANNOTATION_PREFIX="io-limit"
```

### 2. 监控间隔
```bash
# 设置合适的监控间隔，确保状态及时更新
export SMART_LIMIT_MONITOR_INTERVAL=60
```

### 3. 历史窗口
```bash
# 设置足够的历史窗口，确保有足够数据计算趋势
export SMART_LIMIT_HISTORY_WINDOW=120
```

## 注意事项

### 1. 性能影响
- 启动时会扫描所有 Pod，可能影响启动时间
- 建议在节点 Pod 数量较多时适当调整扫描策略

### 2. 数据一致性
- 恢复的状态可能与当前 IO 趋势不匹配
- 系统会在下一个监控周期重新评估限速条件

### 3. 边界情况
- 如果 Pod 注解格式不正确，会跳过恢复
- 已解除限速的 Pod 不会被恢复
- 容器 ID 解析失败时会跳过该容器

## 总结

状态恢复机制确保了：
- **连续性**：Pod 重启后限速状态不丢失
- **一致性**：内存状态与 Pod 注解保持一致
- **可靠性**：避免重复限速和不必要的更新
- **可观测性**：完整的日志记录和状态跟踪

这个机制大大提高了系统的可靠性和用户体验，确保 IO 限速策略的连续执行。 