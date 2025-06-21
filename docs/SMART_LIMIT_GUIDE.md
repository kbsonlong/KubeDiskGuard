# 智能分级限速指南 (Smart Graded Limit Guide)

本文档详细介绍了 `KubeDiskGuard` 服务中的智能分级限速功能的工作原理、控制逻辑和配置方法。

## 1. 核心理念

传统的IO限速通常采用一个固定的阈值，这种方式无法区分瞬时的IO毛刺和持续的IO高压，容易导致不必要的性能限制或响应滞后。

智能分级限速功能通过分析容器在**多个时间窗口（15分钟、30分钟、60分钟）**内的IO趋势，来更精准地判断容器的IO模式，从而做出更合理的限速决策。

- **15分钟窗口**: 关注**近期**的IO活动，能快速响应短时、剧烈的IO爆发。
- **30分钟窗口**: 关注中等时间范围内的IO压力。
- **60分钟窗口**: 关注长期的、持续性的IO负载，用于识别那些长时间占用高IO的"慢热型"应用。

## 2. 控制策略逻辑

智能限速的控制逻辑分为两个核心部分：**触发限速**和**解除限速**。

### 2.1. 触发限速：严格优先原则

当系统检测到一个容器的IO使用率超过阈值时，它会遵循一个明确的优先级顺序来决定采用哪一套限速策略。

**优先级顺序：`15分钟 > 30分钟 > 60分钟`**

系统会按照这个从高到低的优先级依次检查：

1.  **首先，检查15分钟窗口**：
    - 如果15分钟的平均IO超过了15分钟的阈值 (`smart_limit_io_threshold_15m`)，系统会**立即采用15分钟的限速策略**（使用 `smart_limit_iops_limit_15m` 和 `smart_limit_bps_limit_15m` 的值），并**停止后续检查**。

2.  **然后，检查30分钟窗口**：
    - 只有在15分钟窗口的IO**未**超过阈值的情况下，系统才会继续检查30分钟窗口。
    - 如果30分钟的平均IO超过了30分钟的阈值，系统将采用30分钟的限速策略，并停止检查。

3.  **最后，检查60分钟窗口**：
    - 只有在15分钟和30分钟窗口的IO都**未**超过阈值时，系统才会检查60分钟窗口。
    - 如果60分钟的平均IO超过了60分钟的阈值，系统将采用最宽松的60分钟限速策略。

#### 场景分析：

- **场景A：IO突然爆发**
  - **现象**: 15分钟IO > 阈值, 30分钟IO < 阈值, 60分钟IO < 阈值
  - **决策**: 由于15分钟窗口优先级最高，系统将立即采用**15分钟的严格限速策略**。

- **场景B：IO洪峰已过，但中期压力仍在**
  - **现象**: 15分钟IO < 阈值, 30分钟IO > 阈值, 60分钟IO < 阈值
  - **决策**: 系统跳过15分钟检查，发现30分钟窗口"越线"，因此采用**30分钟的中等限速策略**。它不会再去看60分钟的情况。

这种"严格优先"的设计确保了系统能够对最紧急、最剧烈的IO波动做出最快速的响应，优先保障整个节点的磁盘性能稳定。

### 2.2. 解除限速：全体通过原则 (粘性逻辑)

一旦一个容器被限速，这个限速状态就是"粘性的"(Sticky)。系统不会因为短期IO下降就轻易地"降级"或解除限速，以防止因IO抖动造成频繁的策略切换。

要解除限速，必须同时满足以下**所有条件**：

1.  **IO条件：所有窗口均低于解除阈值**
    - 容器在**所有三个时间窗口（15m, 30m, 60m）**的平均IO使用率，都必须低于设定的解除限速阈值 (`smart_limit_remove_threshold`)。
    - 这是一个严格的 `AND` 条件。只要还有一个窗口的IO高于解除阈值，限速就会继续维持。

2.  **时间条件1：满足解除延迟 (`Remove Delay`)**
    - 从上次**施加/更新**限速开始，必须经过一段指定的时间（由 `smart_limit_remove_delay` 配置，单位分钟）。
    - 这个机制能有效防止在一个IO高峰刚被限制后，因IO瞬间回落而立即解除限速，避免"抖动"。

3.  **时间条件2：满足检查间隔 (`Check Interval`)**
    - 为了减少不必要的Pod Annotation更新和API Server请求，系统只会在距离上次检查超过一定间隔（由 `smart_limit_remove_check_interval` 配置，单位分钟）后，才再次尝试执行解除操作。

**总结**：解除限速是一个比触发限速更"谨慎"的操作。它要求容器的IO活动在所有时间维度上都已恢复平稳，并且稳定了足够长的时间。

## 3. 配置项详解

在配置文件中，以下参数与智能分级限速相关：

| 配置项 | 默认值 | 单位 | 描述 |
| :--- | :--- | :--- | :--- |
| `smart_limit_graded_thresholds` | `false` | 布尔 | 是否启用分级限速模式。**必须设为 `true` 才能使用以下策略**。|
| `smart_limit_io_threshold_15m` | `0.8` | 比例 | 15分钟窗口的IOPS触发阈值。 |
| `smart_limit_bps_threshold_15m` | `0.8` | 比例 | 15分钟窗口的BPS触发阈值。 |
| `smart_limit_iops_limit_15m` | `0` | IOPS | 触发15分钟策略后，施加的IOPS限制值。 |
| `smart_limit_bps_limit_15m` | `0` | BPS | 触发15分钟策略后，施加的BPS限制值。 |
| `smart_limit_io_threshold_30m` | `0.8` | 比例 | 30分钟窗口的IOPS触发阈值。 |
| `smart_limit_bps_threshold_30m`| `0.8` | 比例 | 30分钟窗口的BPS触发阈值。 |
| `smart_limit_iops_limit_30m` | `0` | IOPS | 触发30分钟策略后，施加的IOPS限制值。 |
| `smart_limit_bps_limit_30m` | `0` | BPS | 触发30分钟策略后，施加的BPS限制值。 |
| `smart_limit_io_threshold_60m` | `0.8` | 比例 | 60分钟窗口的IOPS触发阈值。 |
| `smart_limit_bps_threshold_60m`| `0.8` | 比例 | 60分钟窗口的BPS触发阈值。 |
| `smart_limit_iops_limit_60m` | `0` | IOPS | 触发60分钟策略后，施加的IOPS限制值。 |
| `smart_limit_bps_limit_60m` | `0` | BPS | 触发60分钟策略后，施加的BPS限制值。 |
| `smart_limit_remove_threshold` | `0.5` | 比例 | 所有窗口IO均需低于此阈值才能解除限速。 |
| `smart_limit_remove_delay` | `5` | 分钟 | 从限速被施加到可以开始检查解除的最小延迟。 |
| `smart_limit_remove_check_interval` | `1` | 分钟 | 执行解除限速检查的最小时间间隔。 |

## 4. 最佳实践与配置建议

1.  **阈值设置应有梯度**：
    - `15m阈值` 应最敏感（例如 `0.7` 或 `70%`），用于捕捉突发。
    - `30m阈值` 可以适中（例如 `0.8` 或 `80%`）。
    - `60m阈值` 应最不敏感（例如 `0.9` 或 `90%`），只用于捕捉长期高负载。

2.  **限速值应有梯度**：
    - `15m限速值` 应最严格，以快速控制住IO风暴。
    - `30m限速值` 应适中。
    - `60m限速值` 应最宽松，给长期运行的任务留出足够空间。

3.  **解除阈值应显著低于触发阈值**：
    - `smart_limit_remove_threshold` 通常建议设置为最低触发阈值（即15m阈值）的70%-80%左右，以建立一个清晰的"安全区"，防止在阈值边缘反复横跳。

4.  **监控与调优**：
    - **初始阶段**：建议先开启监控，但不设置具体的限速值（保持为0），通过观察Pod注解中记录的 `triggered-by` 和IO趋势数据，来了解集群中应用的真实IO模式。
    - **调优阶段**：根据观察到的数据，逐步为不同时间窗口设置合理的限速值，并持续监控限速和解除的频率，找到最佳平衡点。

## 智能限速功能使用指南

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
| `SMART_LIMIT_ANNOTATION_PREFIX` | io-limit | 注解前缀 |

## 使用示例

### 1. 启用智能限速

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: io-limit-service
spec:
  template:
    spec:
      containers:
      - name: io-limit-service
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
    io-limit/smart-limit: "true"           # 标识为智能限速
    io-limit/auto-iops: "800"              # 自动计算的IOPS值
    io-limit/auto-bps: "1048576"           # 自动计算的BPS值
    io-limit/limit-reason: "high-io-detected" # 限速原因
```

### 3. 测试高 IO 场景

创建测试 Pod 来验证智能限速功能：

```bash
# 部署测试 Pod
kubectl apply -f examples/test-pod.yaml

# 查看 Pod 状态
kubectl get pods -l app=high-io-test

# 查看服务日志
kubectl logs -n kube-system -l app=io-limit-service -f

# 检查 Pod 注解
kubectl get pod high-io-test -o yaml | grep -A 10 annotations
```

## 监控和调试

### 1. 查看服务日志

```bash
# 查看智能限速相关日志
kubectl logs -n kube-system -l app=io-limit-service | grep -i "smart"

# 查看 IO 监控日志
kubectl logs -n kube-system -l app=io-limit-service | grep -i "io"
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
kubectl get configmap -n kube-system io-limit-config -o yaml

# 查看服务日志
kubectl logs -n kube-system -l app=io-limit-service | grep -i "smart"

# 检查 Pod 是否被过滤
kubectl logs -n kube-system -l app=io-limit-service | grep -i "skip"
```

### 2. 限速值不合理

**可能原因**：
- 历史数据不足
- 计算逻辑错误
- 阈值设置不当

**排查步骤**：
```bash
# 查看 IO 趋势计算日志
kubectl logs -n kube-system -l app=io-limit-service | grep -i "trend"

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

## 附录：策略设计哲学——"严格优先" vs "宽松优先"

`KubeDiskGuard` 目前采用的是"严格优先" (`15m > 30m > 60m`) 的控制策略。理解其背后的设计考量，有助于更好地运用此工具。

### A.1 当前策略："严格优先" (Strict-First)

这种模式将系统的**快速响应能力**和**稳定性**置于最高优先级。

#### 优点

- **响应敏捷，控制力强**：对于任何突发的、剧烈的IO风暴（无论是应用BUG还是恶意行为），系统都能在最短时间内（15分钟窗口）捕获并施加最严格的限制，防止节点整体性能被拖垮。
- **有效抑制短时脉冲**：能够精准地管理那些持续时间不长但强度极高的IO操作，这正是IO限速需要解决的核心痛点之一。
- **保障多租户环境公平性**：在共享资源的环境中，能迅速遏制单个"坏邻居"的破坏性行为，保护同一节点上其他服务的正常运行。

#### 缺点

- **可能"误判"**：对于一些启动时需要大量IO、但后续表现平稳的正常应用，可能会在启动阶段被短暂地严格限速，影响其启动速度。
- **可能产生抖动**：如果阈值设置不当，对于在阈值边缘波动的IO模式，可能会导致较为频繁的限速和解除操作。

### A.2 备选策略："宽松优先" (Loose-First)

如果我们将优先级反转 (`60m > 30m > 15m`)，系统的行为模式会截然不同。

#### 优点

- **运行更平稳**：对短暂的IO高峰更有"耐心"，不会轻易触发限速，从而减少了策略切换的频率，使系统整体IO曲线更平滑。
- **对长时任务更友好**：允许那些需要持续高IO的、非恶意的后台任务（如数据同步、批量计算）在大部分时间内以较高性能运行，只在确认其构成"长期威胁"时才介入。

#### 缺点

- **响应滞后，风险极高**：这是其最致命的缺陷。系统需要等待很长时间（可能长达数十分钟）才能确认一次真正的IO攻击。在这段"反应滞后期"内，节点的磁盘性能可能已被完全占满，导致雪崩式的服务故障。
- **失去对短期风险的控制**：完全忽略了短期的IO风暴，放弃了作为"哨兵"的核心价值。

### 结论

尽管"宽松优先"策略在某些特定场景下能提供更平稳的性能表现，但它以牺牲**对突发风险的核心控制能力**为代价。在复杂、高密度的生产级容器环境中，稳定性和快速响应能力至关重要。因此，`KubeDiskGuard` 选择了"严格优先"的设计哲学，以确保在任何情况下都能优先保障平台的整体稳定。

## 附录B：IO趋势计算逻辑与数学原理

### B.1 计算目标

智能限速的核心在于**准确评估容器在不同时间窗口内的平均IOPS/BPS趋势**。本节详细介绍其数学计算方法。

### B.2 数学化公式

假设在某个时间窗口内有 \( n+1 \) 个采样点，分别为 \((t_0, v_0), (t_1, v_1), ..., (t_n, v_n)\)，其中：
- \( t_i \)：第 \(i\) 个采样点的时间戳（秒）
- \( v_i \)：第 \(i\) 个采样点的累计IO计数（如累计ReadIOPS）

则每两个相邻采样点之间的**区间速率**为：

\[
r_i = \frac{v_i - v_{i-1}}{t_i - t_{i-1}}
\]
其中 \( i = 1, 2, ..., n \)

窗口内的**平均速率**（如平均IOPS）为：

\[
\text{AvgRate} = \frac{1}{k} \sum_{i=1}^{n} r_i
\]
其中 \( k \) 是有效区间的数量（即 \( t_i \) 在窗口内的区间数）。

### B.3 代码实现与公式对应

- `readIOPS := stats[i].ReadIOPS - stats[i-1].ReadIOPS` → \( v_i - v_{i-1} \)
- `timeDiff := stats[i].Timestamp.Sub(stats[i-1].Timestamp).Seconds()` → \( t_i - t_{i-1} \)
- `float64(readIOPS) / timeDiff` → \( r_i \)
- `totalReadIOPS += ...` → 累加所有 \( r_i \)
- `*interval.readIOPS = float64(totalReadIOPS) / float64(count)` → 取平均

### B.4 举例说明

假设我们有如下采样数据（单位：秒，IOPS为累计值）：

| 采样点 | 时间戳 \( t \) | 累计ReadIOPS \( v \) |
|--------|---------------|----------------------|
| 0      | 0             | 0                    |
| 1      | 60            | 120                  |
| 2      | 120           | 240                  |
| 3      | 180           | 390                  |
| 4      | 240           | 510                  |

假设我们要计算最近4分钟（240秒）内的平均IOPS。

**步骤1：计算每个区间的速率**

- 区间1（0~60s）：\( r_1 = \frac{120-0}{60-0} = 2 \) IOPS
- 区间2（60~120s）：\( r_2 = \frac{240-120}{120-60} = 2 \) IOPS
- 区间3（120~180s）：\( r_3 = \frac{390-240}{180-120} = 2.5 \) IOPS
- 区间4（180~240s）：\( r_4 = \frac{510-390}{240-180} = 2 \) IOPS

**步骤2：计算平均速率**

\[
\text{AvgRate} = \frac{2 + 2 + 2.5 + 2}{4} = \frac{8.5}{4} = 2.125 \text{ IOPS}
\]

### B.5 设计优点

- **抗抖动**：区间速率平均法对采样间隔不均、数据抖动有较强鲁棒性。
- **适应不规则采样**：即使采样间隔不等，也能准确反映趋势。
- **统计更准确**：比简单的"首尾差/总时长"更能反映真实IO变化。 

## 附录C：阈值（IOThreshold）与平均速率的比较原理

### C.1 为什么用平均速率与阈值比较？

- `calculateIOTrend` 计算的是每个时间窗口（15m/30m/60m）内的**平均IOPS/BPS速率**。
- `SmartLimitIOThreshold*` 配置项本质上就是**平均速率的上限**。
- 只有当某个窗口的平均IOPS/BPS超过对应阈值时，才会触发限速。

#### 设计合理性
- 平均速率能平滑掉偶发抖动，反映真实IO趋势。
- 以平均速率与阈值比较，能避免短时毛刺导致的误判，也能及时捕捉到持续高IO。
- 这种方式在业界（如Prometheus、K8s HPA等）也是常见的资源趋势判定方法。

#### 代码示例
```go
if trend.ReadIOPS15m > m.config.SmartLimitIOThreshold15m || ... {
    // 触发限速
}
```

### C.2 IOThreshold 的最大值是多少？

- 阈值的单位是**比例**（如0.8），通常表示"磁盘最大能力的百分比"。
- 理论最大值为1.0（100%），即磁盘的最大能力。
- 实际配置时**不建议设置为1.0**，因为磁盘在极限时会出现延迟抖动、队列堆积等问题。
- **推荐值**：一般建议设置在0.7 ~ 0.9之间（即70% ~ 90%），以留有安全裕度。
- 阈值没有硬性上限，用户可以配置为任意正数，但超过1.0没有实际意义。

### C.3 总结

- `calculateIOTrend` 计算的是平均速率，与 `SmartLimitIOThreshold`（平均速率阈值）比较是**完全合理且业界通用的做法**。
- 阈值最大建议不超过1.0（100%），实际建议设置为0.7 ~ 0.9（70% ~ 90%），以保证系统稳定性和响应能力。 