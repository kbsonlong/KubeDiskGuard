# kubelet API 集成总结

## 问题背景

用户询问是否可以通过 kubelet API 获取 cAdvisor 数据来计算 IOPS 和 BPS 进行智能限速。

## 解决方案

我们实现了完整的 kubelet API 集成方案，为 smartlimit 功能提供了更强大和可靠的数据获取能力。

## 实现内容

### 1. kubelet API 客户端 (`pkg/kubelet/kubelet.go`)

- **KubeletClient**: 封装了 kubelet API 的访问逻辑
- **数据获取方法**:
  - `GetNodeSummary()`: 获取节点摘要统计（包含容器IO数据）
  - `GetCadvisorMetrics()`: 获取 cAdvisor Prometheus 格式指标
  - `ParseCadvisorMetrics()`: 解析 cAdvisor 指标数据
- **数据转换**: 将 kubelet API 数据转换为内部 IOStats 格式

### 2. 配置扩展 (`pkg/config/config.go`)

新增 kubelet API 相关配置项：
- `KubeletTokenPath`: kubelet token 路径
- `KubeletCAPath`: kubelet CA 证书路径  
- `KubeletSkipVerify`: 是否跳过证书验证
- `SmartLimitUseKubeletAPI`: 是否使用 kubelet API 获取IO数据

### 3. smartlimit 模块增强 (`pkg/smartlimit/smartlimit.go`)

- **多数据源支持**: 优先使用 kubelet API，失败时回退到 cgroup 采样
- **数据获取策略**:
  1. 节点摘要 API (`/stats/summary`) - 最完整的数据
  2. cAdvisor 指标 (`/metrics/cadvisor`) - Prometheus 格式
  3. cgroup 文件系统 - 兼容性回退方案

### 4. 测试工具

- **脚本测试**: `scripts/test-kubelet-api.sh` 和 `scripts/test-kubelet-api-advanced.sh`
- **Go 测试程序**: `cmd/test-kubelet-api/main.go`
- **功能验证**: 连接测试、数据获取测试、指标解析测试

### 5. 文档完善

- **集成指南**: `docs/KUBELET_API_INTEGRATION.md`
- **配置示例**: 更新了 `examples/smart-limit-example.yaml`
- **README 更新**: 添加了 kubelet API 相关说明

## 技术优势

### 1. 数据质量
- **实时性更好**: kubelet API 提供实时的容器统计信息
- **数据更完整**: 包含 CPU、内存、磁盘 IO 等多维度数据
- **结构化数据**: JSON 格式，易于解析和处理

### 2. 可靠性
- **API 稳定性**: kubelet API 是 Kubernetes 官方接口
- **错误处理**: 更好的错误处理和重试机制
- **回退机制**: 当 kubelet API 不可用时，自动回退到 cgroup 采样

### 3. 性能
- **批量获取**: 一次 API 调用获取所有容器数据
- **减少文件 I/O**: 避免频繁读取 cgroup 文件
- **缓存机制**: kubelet 内部有数据缓存

## 使用方式

### 1. 启用 kubelet API

```yaml
env:
  - name: SMART_LIMIT_USE_KUBELET_API
    value: "true"
  - name: KUBELET_HOST
    value: "localhost"
  - name: KUBELET_PORT
    value: "10250"
  - name: KUBELET_SKIP_VERIFY
    value: "true"
  - name: KUBELET_TOKEN_PATH
    value: "/var/run/secrets/kubernetes.io/serviceaccount/token"
  - name: KUBELET_CA_PATH
    value: "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
```

### 2. 测试连接

```bash
# 使用脚本测试
./scripts/test-kubelet-api-advanced.sh

# 使用 Go 程序测试
go run cmd/test-kubelet-api/main.go --host=localhost --port=10250 --skip-verify=true
```

### 3. 验证功能

```bash
# 查看服务日志
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "kubelet"

# 检查是否使用 kubelet API
kubectl logs -n kube-system -l app=iops-limit-service | grep -i "kubelet client initialized"
```

## 数据源对比

| 特性 | kubelet API | cgroup 文件系统 |
|------|-------------|-----------------|
| **数据格式** | JSON 结构化 | 原始文本 |
| **实时性** | 高 | 中 |
| **数据完整性** | 高（多维度） | 中（仅IO） |
| **可靠性** | 高（官方API） | 中（文件依赖） |
| **性能** | 高（批量获取） | 中（逐个读取） |
| **兼容性** | 中（需要kubelet） | 高（通用） |
| **错误处理** | 好 | 一般 |

## 故障排查

### 常见问题

1. **连接失败**: 检查 kubelet 服务状态和端口配置
2. **认证失败**: 检查 token 和 CA 证书配置
3. **数据获取失败**: 检查 kubelet 版本和 API 支持

### 排查命令

```bash
# 检查 kubelet 状态
systemctl status kubelet

# 测试 API 连接
curl -k https://localhost:10250/healthz

# 查看 kubelet 日志
journalctl -u kubelet -f
```

## 总结

通过 kubelet API 集成，smartlimit 功能获得了：

1. **更可靠的数据源**: 使用 Kubernetes 官方 API
2. **更丰富的数据**: 多维度容器统计信息
3. **更好的性能**: 批量数据获取，减少 I/O 开销
4. **更强的兼容性**: 自动回退机制确保服务可用性

这个方案完美回答了用户的问题：**是的，smartlimit 可以通过 kubelet API 获取 cAdvisor 数据来计算 IOPS 和 BPS 进行智能限速**，而且我们提供了完整的实现和回退机制。 