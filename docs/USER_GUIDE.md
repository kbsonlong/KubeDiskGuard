<p align="center">
  <img src="./logo.svg" width="120" alt="KubeDiskGuard Logo"/>
</p>

<h1 align="center">KubeDiskGuard</h1>
<p align="center">Kubernetes 节点级磁盘 IO 资源守护与限速服务</p> 

---

# 用户手册（User Guide）

## 产品简介
Kubernetes NVMe IOPS/BPS 限速服务是一款以 DaemonSet 方式部署在每个节点的高性能资源管控工具，支持通过注解和环境变量灵活限制容器磁盘 IOPS/BPS，保障节点 IO 稳定性。

## 核心功能
- 以Pod为主索引，精准限速，避免全量遍历容器运行时
- 支持多种注解和环境变量，声明式配置IOPS/BPS限速
- 支持cgroup v1/v2，兼容Docker/containerd
- 自动监听Pod事件，动态调整限速
- 健康检查、详细日志、完善的单元测试

## 典型使用场景
- 防止单个业务容器高IO影响节点整体性能
- 多租户环境下保障关键业务磁盘IO
- 需要动态调整或解除限速的场景

## 注解与环境变量配置
### 注解（Pod.metadata.annotations）
- `iops-limit/read-iops`：读IOPS限制
- `iops-limit/write-iops`：写IOPS限制
- `iops-limit/iops`：读写IOPS统一限制
- `iops-limit/read-bps`：读带宽限制（字节/秒）
- `iops-limit/write-bps`：写带宽限制（字节/秒）
- `iops-limit/bps`：读写带宽统一限制

**优先级说明**：
- `read-iops`/`write-iops` > `iops`
- `read-bps`/`write-bps` > `bps`
- 注解为0表示解除对应方向的限速

**示例**：
```yaml
annotations:
  iops-limit/read-iops: "1200"
  iops-limit/write-iops: "800"
  iops-limit/iops: "1000"
  iops-limit/read-bps: "10485760"   # 10MB/s
  iops-limit/write-bps: "5242880"   # 5MB/s
  iops-limit/bps: "8388608"         # 8MB/s
```

### 环境变量
| 变量 | 说明 | 默认值 |
|------|------|--------|
| CONTAINER_READ_IOPS_LIMIT | 全局读IOPS限制 | 500 |
| CONTAINER_WRITE_IOPS_LIMIT | 全局写IOPS限制 | 500 |
| CONTAINER_IOPS_LIMIT | 兼容老配置，读写都用 | 500 |
| CONTAINER_READ_BPS_LIMIT | 全局读带宽限制 | 0 |
| CONTAINER_WRITE_BPS_LIMIT | 全局写带宽限制 | 0 |
| DATA_MOUNT | 数据盘挂载点 | /data |
| NODE_NAME | 节点名，建议Downward API注入 |  |

### 过滤机制
- 关键字过滤：`EXCLUDE_KEYWORDS`，如 `pause,istio-proxy`
- 命名空间过滤：`EXCLUDE_NAMESPACES`，如 `kube-system,monitoring`
- LabelSelector过滤：`EXCLUDE_LABEL_SELECTOR`，如 `app=system,env in (prod,staging),!debug`

### 常见问题与FAQ
1. **注解变更多久生效？**
   - 通常几秒内自动生效，依赖K8s事件分发。
2. **如何解除限速？**
   - 注解值设为0即可自动解除对应方向限速。
3. **支持多数据盘吗？**
   - 当前主支持单数据盘挂载点，如需多盘可扩展。
4. **服务重启后限速会丢失吗？**
   - 不会，服务会自动同步Pod注解与现有容器限速。
5. **需要特权模式吗？**
   - 需要，需访问cgroup和容器运行时。

### 故障排查与支持
- 检查服务日志，确认事件监听和限速下发是否正常
- 检查cgroup路径、挂载点、权限
- 查看[CHANGELOG.md](./CHANGELOG.md)和[开发手册](./DEV_GUIDE.md)获取更多信息

---
如有更多问题，请联系运维支持团队或提交issue。 