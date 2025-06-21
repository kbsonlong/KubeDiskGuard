# 智能限速功能使用指南

## 概述

智能限速功能是 KubeDiskGuard 的核心特性之一，能够自动监控容器的 IO 使用情况，当检测到长时间高 IO 时，自动为 Pod 添加限速注解，实现智能化的 IO 资源管理。

## 工作原理

### 1. IO 监控
- 定期从 cgroup 文件系统读取容器的 IO 统计信息
- 支持 cgroup v1 和 v2 两种版本
- 收集读写 IOPS 和 BPS 数据

### 2. 趋势分析
- 计算 15 分钟、30 分钟、60 分钟内的平均 IO 使用情况
- 分析 IOPS 和 BPS 的变化趋势
- 识别长时间高 IO 的容器

### 3. 智能限速
- 当 IO 使用超过阈值时，自动计算合适的限速值
- 为 Pod 添加智能限速注解
- 限速值通常设置为当前平均值的 80%

## 配置参数

### 基础配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `SMART_LIMIT_ENABLED` | false | 是否启用智能限速功能 |
| `SMART_LIMIT_MONITOR_INTERVAL` | 60 | 监控间隔（秒） |
| `SMART_LIMIT_HISTORY_WINDOW` | 10 | 历史数据窗口（分钟） |

### 阈值配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `SMART_LIMIT_HIGH_IO_THRESHOLD` | 0.8 | 高 IOPS 阈值 |
| `SMART_LIMIT_HIGH_BPS_THRESHOLD` | 0.8 | 高 BPS 阈值（字节/秒） |

### 限速配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `SMART_LIMIT_AUTO_IOPS` | 0 | 最小 IOPS 限速值（0表示基于当前IO计算） |
| `SMART_LIMIT_AUTO_BPS` | 0 | 最小 BPS 限速值（0表示基于当前IO计算） |
| `SMART_LIMIT_ANNOTATION_PREFIX` | iops-limit | 注解前缀 |

## 使用示例

### 1. 启用智能限速

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: iops-limit-service
spec:
  template:
    spec:
      containers:
      - name: iops-limit-service
        env:
        # 启用智能限速
        - name: SMART_LIMIT_ENABLED
          value: "true"
        # 监控间隔60秒
        - name: SMART_LIMIT_MONITOR_INTERVAL
          value: "60"
        # 历史数据窗口10分钟
        - name: SMART_LIMIT_HISTORY_WINDOW
          value: "10"
        # 高IO阈值1000 IOPS
        - name: SMART_LIMIT_HIGH_IO_THRESHOLD
          value: "1000"
        # 高BPS阈值1MB/s
        - name: SMART_LIMIT_HIGH_BPS_THRESHOLD
          value: "1048576"
        # 最小IOPS限速值
        - name: SMART_LIMIT_AUTO_IOPS
          value: "500"
        # 最小BPS限速值
        - name: SMART_LIMIT_AUTO_BPS
          value: "524288"
```

### 2. 智能限速注解

当检测到高 IO 时，系统会自动为 Pod 添加以下注解：

```yaml
metadata:
  annotations:
    iops-limit/smart-limit: "true"           # 标识为智能限速
    iops-limit/auto-iops: "800"              # 自动计算的IOPS值
    iops-limit/auto-bps: "1048576"           # 自动计算的BPS值
    iops-limit/limit-reason: "high-io-detected" # 限速原因
```

### 3. 测试高 IO 场景

创建测试 Pod 来验证智能限速功能：

```bash
# 部署测试 Pod
kubectl apply -f examples/test-pod.yaml

# 查看 Pod 状态
kubectl get pods -l app=high-io-test

# 查看服务日志
kubectl logs -n kube-system -l app=iops-limit-service -f

# 检查 Pod 注解
kubectl get pod high-io-test -o yaml | grep -A 10 annotations
```

## 监控和调试

### 1. 查看服务日志

```bash
# 查看智能限速相关日志
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "smart"

# 查看 IO 监控日志
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "io"
```

### 2. 检查 cgroup 统计

```bash
# 进入节点查看 cgroup 统计
# cgroup v1
cat /sys/fs/cgroup/blkio/docker/<container-id>/blkio.throttle.io_serviced
cat /sys/fs/cgroup/blkio/docker/<container-id>/blkio.throttle.io_service_bytes

# cgroup v2
cat /sys/fs/cgroup/<container-id>/io.stat
```

### 3. 验证限速效果

```bash
# 检查 cgroup 限速设置
# cgroup v1
cat /sys/fs/cgroup/blkio/docker/<container-id>/blkio.throttle.read_iops_device
cat /sys/fs/cgroup/blkio/docker/<container-id>/blkio.throttle.write_iops_device

# cgroup v2
cat /sys/fs/cgroup/<container-id>/io.max
```

## 最佳实践

### 1. 阈值设置

- **IOPS 阈值**：根据磁盘性能设置，通常为磁盘最大 IOPS 的 70-80%
- **BPS 阈值**：根据磁盘带宽设置，通常为磁盘最大带宽的 70-80%
- **监控间隔**：建议 30-120 秒，平衡实时性和性能
- **历史窗口**：建议 10-30 分钟，确保有足够的历史数据

### 2. 过滤配置

```yaml
# 排除系统容器和已知的高 IO 应用
- name: EXCLUDE_KEYWORDS
  value: "pause,istio-proxy,psmdb,kube-system,koordinator,apisix"

# 排除特定命名空间
- name: EXCLUDE_NAMESPACES
  value: "kube-system,monitoring,logging"

# 排除特定标签的 Pod
- name: EXCLUDE_LABEL_SELECTOR
  value: "app=system,env in (prod,staging),!debug"
```

### 3. 资源限制

```yaml
# 为服务设置合理的资源限制
resources:
  requests:
    memory: "64Mi"
    cpu: "50m"
  limits:
    memory: "128Mi"
    cpu: "100m"
```

## 故障排查

### 1. 智能限速未生效

**可能原因**：
- 智能限速功能未启用
- 阈值设置过高
- 容器被过滤规则排除
- cgroup 统计读取失败

**排查步骤**：
```bash
# 检查配置
kubectl get configmap -n kube-system iops-limit-config -o yaml

# 查看服务日志
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "smart"

# 检查 Pod 是否被过滤
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "skip"
```

### 2. 限速值不合理

**可能原因**：
- 历史数据不足
- 计算逻辑错误
- 阈值设置不当

**排查步骤**：
```bash
# 查看 IO 趋势计算日志
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "trend"

# 检查 cgroup 统计数据
kubectl exec -n kube-system <pod-name> -- cat /sys/fs/cgroup/io.stat
```

### 3. 性能影响

**可能原因**：
- 监控间隔过短
- 历史数据窗口过大
- 资源限制过小

**优化建议**：
- 增加监控间隔到 60-120 秒
- 减少历史数据窗口到 10-15 分钟
- 适当增加资源限制

## 注意事项

1. **权限要求**：服务需要特权模式运行，访问 cgroup 文件系统
2. **兼容性**：支持 cgroup v1 和 v2，自动检测
3. **性能影响**：监控会消耗少量 CPU 和内存资源
4. **数据持久性**：历史数据存储在内存中，服务重启会丢失
5. **限速精度**：基于 cgroup 统计，可能存在一定误差

## 扩展功能

### 1. 自定义限速算法

可以通过修改 `calculateLimitIOPS` 和 `calculateLimitBPS` 方法来实现自定义的限速算法。

### 2. 多维度监控

可以扩展监控维度，包括：
- 磁盘延迟监控
- IO 队列深度监控
- 磁盘利用率监控

### 3. 告警集成

可以集成 Prometheus 和 AlertManager 来实现告警功能：
- 高 IO 告警
- 限速生效告警
- 服务异常告警 