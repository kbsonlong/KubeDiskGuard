# Kubernetes NVMe 磁盘 IOPS 限速服务

这是一个用 Go 语言编写的 Kubernetes 服务，用于自动限制容器对 NVMe 磁盘的 IOPS 访问，防止单个容器的高 IO 操作影响宿主机性能。

## 功能特性

- **自动检测容器运行时**：支持 Docker 和 containerd
- **自动检测 cgroup 版本**：支持 cgroup v1 和 v2
- **实时事件监听**：监听容器创建事件，自动为新容器设置 IOPS 限制
- **智能过滤**：自动过滤系统容器（如 pause、istio-proxy 等）
- **配置灵活**：支持环境变量配置所有参数

## 环境要求

- Kubernetes 1.20+
- Linux 内核 4.9+
- Docker 或 containerd 运行时
- cgroup v1 或 v2

## 快速开始

### 1. 构建镜像

```bash
# 构建 Go 服务镜像
docker build -t your-registry/iops-limit-service:latest .

# 推送到镜像仓库
docker push your-registry/iops-limit-service:latest
```

### 2. 部署到 Kubernetes

```bash
# 修改 k8s-daemonset.yaml 中的镜像地址
# 将 your-registry/iops-limit-service:latest 替换为你的镜像地址

# 部署服务
kubectl apply -f k8s-daemonset.yaml
```

### 3. 验证部署

```bash
# 查看 DaemonSet 状态
kubectl get daemonset -n kube-system iops-limit-service

# 查看 Pod 日志
kubectl logs -n kube-system -l app=iops-limit-service -f
```

## 配置参数

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `CONTAINER_IOPS_LIMIT` | 500 | 单个容器的 IOPS 限制 |
| `DATA_TOTAL_IOPS` | 3000 | 数据盘总 IOPS 限制 |
| `DATA_MOUNT` | /data | 数据盘挂载点 |
| `EXCLUDE_KEYWORDS` | pause,istio-proxy,psmdb,kube-system,koordinator,apisix | 排除的容器关键字（逗号分隔） |
| `CONTAINERD_NAMESPACE` | k8s.io | containerd 命名空间 |
| `CONTAINER_RUNTIME` | auto | 容器运行时（auto/docker/containerd） |
| `CGROUP_VERSION` | auto | cgroup 版本（auto/v1/v2） |
| `CHECK_INTERVAL` | 30 | 检查间隔（秒） |

## 工作原理

### 1. 自动检测
- 自动检测容器运行时（Docker 或 containerd）
- 自动检测 cgroup 版本（v1 或 v2）
- 自动获取 NVMe 设备的主次设备号

### 2. 容器过滤
- 根据镜像名和容器名过滤系统容器
- 只对业务容器应用 IOPS 限制

### 3. IOPS 限制
- **cgroup v1**：写入 `blkio.throttle.read_iops_device` 和 `blkio.throttle.write_iops_device`
- **cgroup v2**：写入 `io.max` 文件

### 4. 事件监听
- **Docker**：使用 Docker API 监听容器创建事件
- **containerd**：使用 `ctr events` 监听容器创建事件

## 本地开发

### 1. 安装依赖

```bash
go mod download
```

### 2. 运行服务

```bash
# 设置环境变量
export CONTAINER_IOPS_LIMIT=500
export DATA_MOUNT=/data
export CONTAINER_RUNTIME=docker

# 运行服务
go run main.go
```

### 3. 构建二进制文件

```bash
go build -o iops-limit-service main.go
```

## 测试验证

### 1. 创建测试容器

```bash
# 创建一个测试容器
docker run -d --name test-container -v /data:/data alpine sleep 3600
```

### 2. 验证 IOPS 限制

```bash
# 进入容器测试 IO 性能
docker exec -it test-container sh

# 安装 fio
apk add fio

# 运行 fio 测试
fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting --direct=1
```

### 3. 检查 cgroup 设置

```bash
# 查找容器的 cgroup 路径
find /sys/fs/cgroup/blkio -name "*test-container*"

# 查看 IOPS 限制
cat /sys/fs/cgroup/blkio/docker/[container-id]/blkio.throttle.read_iops_device
cat /sys/fs/cgroup/blkio/docker/[container-id]/blkio.throttle.write_iops_device
```

## 故障排除

### 1. 权限问题

确保容器以特权模式运行：

```yaml
securityContext:
  privileged: true
  runAsUser: 0
  runAsGroup: 0
```

### 2. 设备号获取失败

检查数据盘挂载点：

```bash
df /data
lsblk -no PKNAME $(df /data | tail -1 | awk '{print $1}')
```

### 3. cgroup 路径不存在

检查 cgroup 版本和路径：

```bash
# 检查 cgroup 版本
ls /sys/fs/cgroup/cgroup.controllers

# 查找容器 cgroup 路径
find /sys/fs/cgroup -name "*[container-id]*"
```

### 4. 事件监听失败

检查容器运行时连接：

```bash
# Docker
docker ps

# containerd
ctr --namespace k8s.io containers list
```

## 监控和日志

### 1. 查看服务日志

```bash
kubectl logs -n kube-system -l app=iops-limit-service -f
```

### 2. 监控指标

服务会输出以下日志信息：
- 配置信息
- 容器检测和过滤
- IOPS 限制设置
- 错误信息

### 3. 健康检查

服务包含 liveness 和 readiness 探针，确保服务正常运行。

## 注意事项

1. **NVMe 设备限制**：IOPS 限制只对整个 NVMe 设备生效，不对分区生效
2. **性能影响**：IOPS 限制会影响容器 I/O 性能，请根据实际需求调整
3. **系统容器**：确保正确配置排除关键字，避免影响系统容器
4. **权限要求**：需要特权模式访问 cgroup 和容器运行时

## 许可证

MIT License 