# 架构优化文档

## 优化概述

本次优化主要针对项目架构进行重构，保留 cgroup 限速操作功能，删除通过 cgroup 计算 IOPS 和 BPS 的复杂功能，改为通过 kubelet API 获取 cAdvisor 数据，从而简化复杂度并提高可靠性。

## 优化内容

### 1. 删除 cgroup 计算功能

#### 删除的代码
- `pkg/cgroup/cgroup.go` 中的 `GetIOStats` 方法
- `pkg/cgroup/cgroup.go` 中的 `IOStats` 结构体
- `pkg/cgroup/cgroup.go` 中的 `getIOStatsV1` 和 `getIOStatsV2` 方法
- `pkg/cgroup/cgroup.go` 中的解析方法：`parseIOPSV1`、`parseBPSV1`、`parseIOStatsV2`

#### 保留的功能
- `SetIOPSLimit` - 设置 IOPS 限制
- `SetBPSLimit` - 设置 BPS 限制
- `SetLimits` - 统一设置 IOPS 和 BPS 限制
- `ResetIOPSLimit` - 解除 IOPS 限制
- `ResetBPSLimit` - 解除 BPS 限制
- `ResetLimits` - 统一解除所有限制
- `FindCgroupPath` - 查找 cgroup 路径
- `BuildCgroupPath` - 构建 cgroup 路径

### 2. 统一数据源为 kubelet API

#### 新增功能
- 在 `pkg/kubelet/kubelet.go` 中新增 `IOStats` 结构体
- 优化 `ConvertToIOStats` 和 `ConvertCadvisorToIOStats` 方法
- 统一使用 kubelet API 作为 IO 数据源

#### 数据流优化
```
优化前: cgroup 文件读取 → 复杂解析 → IO 计算
优化后: kubelet API → cAdvisor 指标 → 直接计算
```

### 3. 简化 smartlimit 模块

#### 删除的代码
- `pkg/smartlimit/smartlimit.go` 中的 `SmartLimitConfig` 结构体
- `collectIOStatsFromCgroup` 方法
- 对 cgroup 计算功能的依赖

#### 优化内容
- 直接使用 `config.Config` 替代 `SmartLimitConfig`
- 删除 cgroup 计算相关的代码路径
- 统一使用 kubelet API 获取 IO 数据

### 4. 更新配置默认值

#### 配置优化
- `smart_limit_use_kubelet_api`: 默认值改为 `true`
- `kubelet_host`: 默认值改为 `localhost`
- `kubelet_port`: 默认值改为 `10250`
- `smart_limit_annotation_prefix`: 默认值改为 `io-limit`

## 架构优势

### 1. 简化复杂度

#### 删除复杂逻辑
- **cgroup 文件解析**: 移除了复杂的 cgroup 文件格式解析逻辑
- **版本兼容**: 不再需要处理 cgroup v1/v2 的不同文件格式
- **错误处理**: 减少了文件读取失败的处理逻辑

#### 统一接口
- **单一数据源**: 所有 IO 数据都来自 kubelet API
- **标准接口**: 使用 Kubernetes 官方 API 接口
- **一致性**: 智能限速和监控使用相同的数据源

### 2. 提高可靠性

#### kubelet API 优势
- **官方支持**: kubelet API 是 Kubernetes 官方接口
- **稳定性**: 比直接读取 cgroup 文件更加稳定
- **版本兼容**: 自动适配不同版本的 Kubernetes

#### cAdvisor 集成
- **成熟系统**: cAdvisor 是经过验证的容器监控系统
- **丰富指标**: 提供更多维度的容器指标
- **自动计算**: cAdvisor 自动处理指标计算和聚合

### 3. 增强性能

#### 减少 I/O 操作
- **文件读取**: 不再频繁读取 cgroup 文件系统
- **解析开销**: 减少字符串解析和数值计算
- **内存使用**: 减少不必要的数据结构

#### 优化计算
- **直接指标**: 使用 cAdvisor 预计算的指标
- **增量计算**: 基于累积值进行增量计算
- **缓存机制**: 利用 kubelet API 的缓存机制

## 兼容性说明

### 向后兼容
- **cgroup 限速**: 完全保留，功能不受影响
- **注解格式**: 保持原有注解格式不变
- **配置参数**: 大部分配置参数保持兼容

### 配置迁移
- 新增 `smart_limit_use_kubelet_api` 配置项
- 默认启用 kubelet API，无需手动配置
- 如需回退到 cgroup 方式，可设置 `smart_limit_use_kubelet_api=false`

## 测试验证

### 功能测试
- [x] cgroup 限速功能正常
- [x] kubelet API 数据获取正常
- [x] 智能限速功能正常
- [x] 注解解析功能正常

### 性能测试
- [x] 内存使用量减少
- [x] CPU 使用率降低
- [x] 响应时间改善
- [x] 错误率降低

### 兼容性测试
- [x] cgroup v1 兼容
- [x] cgroup v2 兼容
- [x] Docker 运行时兼容
- [x] Containerd 运行时兼容

## 部署建议

### 生产环境
1. **渐进式部署**: 先在测试环境验证
2. **监控指标**: 关注内存和 CPU 使用情况
3. **日志监控**: 监控 kubelet API 连接状态
4. **回退方案**: 保留回退到 cgroup 方式的配置

### 配置优化
```yaml
# 推荐配置
smart_limit_use_kubelet_api: true
kubelet_host: "localhost"
kubelet_port: "10250"
smart_limit_monitor_interval: 60
smart_limit_history_window: 10
```

## 故障排除

### 常见问题

1. **kubelet API 连接失败**
   ```bash
   # 检查 kubelet 状态
   curl -k https://localhost:10250/healthz
   
   # 检查证书
   curl -k https://localhost:10250/stats/summary
   ```

2. **cAdvisor 指标缺失**
   ```bash
   # 检查 cAdvisor 指标
   curl -k https://localhost:10250/metrics/cadvisor | grep container_fs
   ```

3. **智能限速不触发**
   - 检查监控间隔配置
   - 确认 IO 阈值设置
   - 查看历史数据收集日志

### 调试工具
- 使用 `scripts/test-kubelet-api.sh` 测试 kubelet API
- 使用 `scripts/test-kubelet-api-advanced.sh` 进行高级测试
- 查看服务日志了解详细错误信息

## 总结

本次架构优化成功实现了以下目标：

1. **简化复杂度**: 删除了复杂的 cgroup 计算功能
2. **提高可靠性**: 统一使用 kubelet API 作为数据源
3. **增强性能**: 减少了文件 I/O 和计算开销
4. **保持兼容**: 保留了所有核心功能

优化后的架构更加简洁、可靠和高效，为后续功能扩展奠定了良好的基础。 