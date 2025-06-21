# kubelet API 集成指南

## 概述

KubeDiskGuard 现在支持通过 kubelet API 获取容器的 IO 统计信息，作为 cgroup 文件系统采样的替代方案。这种集成提供了更丰富的数据源和更好的可靠性。

## 优势

### 1. 数据丰富性
- **实时性更好**：kubelet API 提供实时的容器统计信息
- **数据更完整**：包含 CPU、内存、磁盘 IO 等多维度数据
- **结构化数据**：JSON 格式，易于解析和处理

### 2. 可靠性
- **API 稳定性**：kubelet API 是 Kubernetes 官方接口
- **错误处理**：更好的错误处理和重试机制
- **回退机制**：当 kubelet API 不可用时，自动回退到 cgroup 采样

### 3. 性能
- **批量获取**：一次 API 调用获取所有容器数据
- **减少文件 I/O**：避免频繁读取 cgroup 文件
- **缓存机制**：kubelet 内部有数据缓存

## 配置参数

### 基础配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `SMART_LIMIT_USE_KUBELET_API` | false | 是否启用 kubelet API |
| `KUBELET_HOST` | localhost | kubelet 主机地址 |
| `KUBELET_PORT` | 10250 | kubelet 端口 |

### 认证配置

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `KUBELET_TOKEN_PATH` | | kubelet token 路径 |
| `KUBELET_CA_PATH` | | kubelet CA 证书路径 |
| `KUBELET_SKIP_VERIFY` | false | 是否跳过证书验证 |

## 数据源优先级

当启用 kubelet API 时，数据获取按以下优先级进行：

1. **节点摘要 API** (`/stats/summary`)
   - 提供所有 Pod 和容器的统计信息
   - 包含磁盘 IO 数据（如果可用）
   - 数据格式最完整

2. **cAdvisor 指标** (`/metrics/cadvisor`)
   - Prometheus 格式的指标数据
   - 包含详细的磁盘 IO 统计
   - 需要解析 Prometheus 格式

3. **cgroup 文件系统**（回退方案）
   - 当 kubelet API 不可用时使用
   - 直接读取 cgroup 文件
   - 兼容性最好

## 使用示例

### 1. 启用 kubelet API

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
        # 启用 kubelet API
        - name: SMART_LIMIT_USE_KUBELET_API
          value: "true"
        - name: KUBELET_HOST
          value: "localhost"
        - name: KUBELET_PORT
          value: "10250"
        - name: KUBELET_SKIP_VERIFY
          value: "true"
        
        # 认证配置
        - name: KUBELET_TOKEN_PATH
          value: "/var/run/secrets/kubernetes.io/serviceaccount/token"
        - name: KUBELET_CA_PATH
          value: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
```

### 2. 测试 kubelet API 连接

```bash
# 使用测试工具
go run cmd/test-kubelet-api/main.go \
  --host=localhost \
  --port=10250 \
  --skip-verify=true

# 或使用脚本
./scripts/test-kubelet-api-advanced.sh
```

### 3. 验证数据获取

```bash
# 查看服务日志
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "kubelet"

# 检查是否使用 kubelet API
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "kubelet client initialized"
```

## 数据格式对比

### kubelet API 数据格式

```json
{
  "node": {
    "name": "worker-1",
    "timestamp": "2024-01-01T12:00:00Z"
  },
  "pods": [
    {
      "podRef": {
        "name": "nginx-pod",
        "namespace": "default"
      },
      "containers": [
        {
          "name": "nginx",
          "timestamp": "2024-01-01T12:00:00Z",
          "diskio": {
            "readBytes": 1024000,
            "writeBytes": 512000,
            "readIOPS": 100,
            "writeIOPS": 50
          }
        }
      ]
    }
  ]
}
```

### cAdvisor 指标格式

```
# HELP container_fs_reads_total Cumulative count of reads completed
# TYPE container_fs_reads_total counter
container_fs_reads_total{container="nginx",id="/docker/1234567890abcdef"} 100

# HELP container_fs_writes_total Cumulative count of writes completed
# TYPE container_fs_writes_total counter
container_fs_writes_total{container="nginx",id="/docker/1234567890abcdef"} 50
```

## 故障排查

### 1. kubelet API 连接失败

**可能原因**：
- kubelet 服务未运行
- 端口配置错误
- 认证配置错误

**排查步骤**：
```bash
# 检查 kubelet 服务状态
systemctl status kubelet

# 检查端口是否开放
netstat -tlnp | grep 10250

# 测试 API 连接
curl -k https://localhost:10250/healthz
```

### 2. 认证失败

**可能原因**：
- token 文件不存在或权限错误
- CA 证书配置错误
- 证书验证失败

**排查步骤**：
```bash
# 检查 token 文件
ls -la /var/run/secrets/kubernetes.io/serviceaccount/token

# 检查 CA 证书
ls -la /var/run/secrets/kubernetes.io/serviceaccount/ca.crt

# 测试认证
curl -k --cacert /var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
  -H "Authorization: Bearer $(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
  https://localhost:10250/stats/summary
```

### 3. 数据获取失败

**可能原因**：
- kubelet 版本不支持某些 API
- 容器运行时配置问题
- 权限不足

**排查步骤**：
```bash
# 检查 kubelet 版本
kubelet --version

# 查看 kubelet 日志
journalctl -u kubelet -f

# 检查 API 端点
curl -k https://localhost:10250/stats/summary | jq '.'
```

## 性能优化

### 1. 监控间隔调整

```yaml
# 根据集群规模调整监控间隔
- name: SMART_LIMIT_MONITOR_INTERVAL
  value: "120"  # 大集群使用更长间隔
```

### 2. 数据缓存

kubelet API 内部有数据缓存，通常不需要额外的缓存机制。

### 3. 并发控制

服务会自动控制并发请求数量，避免对 kubelet 造成压力。

## 最佳实践

### 1. 生产环境配置

```yaml
# 生产环境建议配置
- name: SMART_LIMIT_USE_KUBELET_API
  value: "true"
- name: KUBELET_SKIP_VERIFY
  value: "false"  # 生产环境启用证书验证
- name: SMART_LIMIT_MONITOR_INTERVAL
  value: "60"     # 合理的监控间隔
```

### 2. 监控和告警

```yaml
# 添加监控指标
- name: KUBELET_API_ERRORS
  value: "0"  # 监控 kubelet API 错误次数
```

### 3. 资源限制

```yaml
resources:
  requests:
    memory: "128Mi"  # kubelet API 需要更多内存
    cpu: "100m"
  limits:
    memory: "256Mi"
    cpu: "200m"
```

## 兼容性说明

### 支持的 kubelet 版本
- Kubernetes 1.16+
- 支持 `/stats/summary` API
- 支持 `/metrics/cadvisor` 端点

### 容器运行时支持
- Docker
- Containerd
- CRI-O

### 操作系统支持
- Linux (所有发行版)
- 需要 kubelet 服务运行

## 总结

kubelet API 集成为 KubeDiskGuard 提供了更强大和可靠的数据获取能力。通过合理配置，可以获得更好的性能和更丰富的监控数据，同时保持与现有 cgroup 采样方案的兼容性。 