# Kubernetes NVMe 磁盘 IOPS 限速服务

这是一个用 Go 语言编写的 Kubernetes DaemonSet 服务，用于自动限制容器对 NVMe 磁盘的 IOPS 访问，防止单个容器的高 IO 操作影响宿主机性能。

## 核心特性

- 自动检测容器运行时（Docker/containerd）和 cgroup 版本（v1/v2）
- **以Pod为主索引，所有限速和过滤逻辑均以Pod+containerStatuses为入口，避免全量遍历容器运行时**
- 通过 client-go 监听本节点 Pod 事件，自动为新容器或注解变更的容器设置/调整 IOPS 限制
- **服务重启时保持IOPS限制一致性**：重启后会自动获取Pod注解信息，确保现有容器的IOPS限制与注解配置保持一致
- **优先使用kubelet API**：减少API Server压力，提高性能和可靠性
- 支持多维度过滤（关键字、命名空间、正则、K8s label selector）
- 支持通过注解动态调整单个 Pod 的 IOPS 限制
- 配置灵活，环境变量可控
- 健康检查、详细日志、单元测试

## 设计原则与架构亮点

- **以Pod为主索引**：所有业务逻辑（限速、过滤、注解变更等）均以Pod及其containerStatuses为入口，极大提升性能和准确性。
- **运行时只做单容器操作**：只在需要底层操作（如cgroup限速）时，用runtime ID查单个容器详细信息，避免全量遍历。
- **事件监听、注解变更、服务重启等场景全部用Pod+containerStatuses实现**，保证与K8s调度状态强一致。
- **代码结构清晰**：service层负责业务主流程和过滤，runtime层只负责单容器操作。

## 架构图

> IOPS Limit Service 以 DaemonSet agent 方式运行在每个 WorkNode 上，通过 client-go 监听 Kubernetes API Server 的 Pod 事件，**并不是替代 kubelet**，而是作为节点的辅助资源管理服务。

```mermaid
flowchart TD
    subgraph "Kubernetes WorkNode"
        direction TB
        Kubelet["Kubelet (原生组件)"]
        Runtime["Docker/Containerd"]
        Service["IOPS Limit Service (DaemonSet)"]
        Cgroup["Cgroup v1/v2"]
        Pod1["Pod (含注解)"]
        Pod2["Pod (含注解)"]
    end
    subgraph "Kubernetes Control Plane"
        APIServer["Kubernetes API Server"]
    end
    APIServer -- "Pod事件/变更" --> Service
    Service -- "查找本地容器/注解" --> Runtime
    Service -- "设置IOPS限制" --> Cgroup
    Runtime -- "管理容器生命周期" --> Cgroup
    Pod1 -. "由Kubelet调度" .-> Runtime
    Pod2 -. "由Kubelet调度" .-> Runtime
    subgraph "管理"
        User["用户/运维"]
    end
    User -- "配置注解/环境变量" --> APIServer
    User -- "部署/管理" --> Service
    Cgroup -- "物理IO限制" --> NVMe["NVMe磁盘"]
```

## 主要优化说明

- **所有限速和过滤逻辑均以Pod为主索引**，只遍历K8s已知的业务容器，极大提升性能和准确性。
- **运行时不再支持GetContainersByPod、全量GetContainers等接口**，只保留GetContainerByID、SetIOPSLimit等单容器操作。
- **事件监听、注解变更、服务重启等场景全部用Pod+containerStatuses实现**，避免无谓的全量遍历。
- **代码职责分明**：service层聚焦业务主流程和过滤，runtime层聚焦单容器底层操作。

## 使用说明

### 1. 注解动态调整 IOPS

在 Pod 的 metadata.annotations 中添加如下注解即可动态调整该 Pod 的 IOPS 限制：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mypod
  annotations:
    iops-limit/limit: "1200"
```

### 2. 过滤机制

- **关键字过滤**：`EXCLUDE_KEYWORDS`，如 `pause,istio-proxy`
- **命名空间过滤**：`EXCLUDE_NAMESPACES`，如 `kube-system,monitoring`
- **LabelSelector过滤**：`EXCLUDE_LABEL_SELECTOR`，支持 K8s 原生 label selector 语法，如 `app=system,env in (prod,staging),!debug`

**示例环境变量配置：**

```yaml
env:
  - name: EXCLUDE_KEYWORDS
    value: "pause,istio-proxy"
  - name: EXCLUDE_NAMESPACES
    value: "kube-system,monitoring"
  - name: EXCLUDE_LABEL_SELECTOR
    value: "app=system,env in (prod,staging),!debug"
```

### 3. 主要环境变量

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `NODE_NAME` |  | 必须，节点名，建议通过Downward API注入 |
| `CONTAINER_IOPS_LIMIT` | 500 | 单个容器的 IOPS 限制 |
| `DATA_MOUNT` | /data | 数据盘挂载点 |
| `EXCLUDE_KEYWORDS` | pause,istio-proxy,psmdb,kube-system,koordinator,apisix | 排除的容器关键字 |
| `EXCLUDE_NAMESPACES` | kube-system | 排除的命名空间 |
| `EXCLUDE_LABEL_SELECTOR` |  | K8s label selector 语法 |
| `CONTAINER_RUNTIME` | auto | 容器运行时 |
| `CONTAINER_SOCKET_PATH` | | 容器运行时 `socket` 地址 |
| `CGROUP_VERSION` | auto | cgroup 版本 |
| `CHECK_INTERVAL` | 30 | 检查间隔（秒） |
| `KUBELET_HOST` | localhost | kubelet API 主机地址 |
| `KUBELET_PORT` | 10250 | kubelet API 端口 |
| `KUBELET_CA_PATH` |  | kubelet API CA证书路径 |
| `KUBELET_CLIENT_CERT_PATH` |  | kubelet API客户端证书路径 |
| `KUBELET_CLIENT_KEY_PATH` |  | kubelet API客户端私钥路径 |
| `KUBELET_TOKEN_PATH` |  | kubelet API Token路径（本地调试可选） |
| `KUBELET_SKIP_VERIFY` |  | kubelet API跳过验证 |

#### DaemonSet注入节点名示例：
```yaml
env:
  - name: NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName
```

### 4. 快速开始

1. 构建镜像并推送到仓库
2. 修改 DaemonSet YAML，配置镜像和环境变量
3. 部署到集群：`kubectl apply -f k8s-daemonset.yaml`
4. 查看日志：`kubectl logs -n kube-system -l app=iops-limit-service -f`

### 5. 验证与排查

- 创建测试容器，使用 fio 验证 IOPS 限制
- 检查 cgroup 路径和限速文件
- 查看服务日志，确认过滤和限速逻辑
- 遇到问题请检查权限、挂载点、cgroup 版本、环境变量配置

## 开发与测试

### 1. 本地开发调试
1. 克隆代码仓库
2. 安装依赖：`go mod download`
3. 配置本地环境变量（可参考上文）
4. 运行服务：`go run main.go`
5. 构建二进制：`go build -o iops-limit-service main.go`
6. 构建镜像：`docker build -t your-repo/iops-limit-service:latest .`

### 2. 单元测试
- 运行所有测试：
  ```bash
  go test -v
  ```
- 你可以参考 `main_test.go` 文件了解更多测试细节。

### 3. 扩展与贡献
- 新增注解支持：在 service.go 中扩展注解解析逻辑
- 支持新运行时：实现 container.Runtime 接口
- 日志与监控：可集成 Prometheus、OpenTelemetry 等
- 贡献代码：Fork、PR、CI 测试

## 故障排查

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

### 4. 日志与监控
查看服务日志：
```bash
kubectl logs -n kube-system -l app=iops-limit-service -f
```
服务会输出配置信息、容器检测和过滤、IOPS 限制设置、错误信息等。

**重要说明**：服务重启时会自动获取Pod注解信息，确保现有容器的IOPS限制与注解配置保持一致。如果无法获取Pod信息（如网络问题），会使用默认配置作为fallback。

### 5. 健康检查
服务包含 liveness 和 readiness 探针，确保服务正常运行。

### 6. 服务重启行为
- **正常情况**：重启后会自动获取本节点所有Running状态的Pod注解，并应用相应的IOPS限制
- **网络异常**：如果无法连接Kubernetes API，会使用默认配置处理现有容器
- **日志标识**：日志中会明确显示是使用Pod特定限制还是默认限制
  - `Applied Pod-specific IOPS limit for container xxx (pod: namespace/name): 1000`
  - `Applied default IOPS limit for container xxx: 500`

### 7. kubelet API 使用说明
- **优先使用**：服务优先使用kubelet API获取Pod信息，减少API Server压力
- **自动回退**：如果kubelet API不可用，自动回退到API Server
- **配置说明**：
  - `KUBELET_HOST`: kubelet服务地址，默认为localhost
  - `KUBELET_PORT`: kubelet API端口，默认为10250
- **日志标识**：
  - `Successfully got X pods from kubelet API`
  - `Failed to get pods from kubelet API: xxx, falling back to API Server`

## 单元测试

本项目已包含完善的单元测试，覆盖注解解析、容器查找、动态限速、过滤逻辑等核心功能。

运行所有测试：

```bash
go test -v
```

## 常见问题与注意事项

- 注解变更后通常几秒内自动生效
- 推荐 K8s 1.20+，理论上 1.16+ 兼容
- 仅主支持单数据盘挂载点，如需多盘可扩展
- IOPS 限制只对整个 NVMe 设备生效，不对分区生效
- 需以特权模式运行，访问 cgroup 和容器运行时
- 正确配置过滤关键字，避免影响系统容器
- 服务包含健康检查和详细日志输出

## 许可证

MIT License 

## 本地开发与调试

### 1. 必要环境变量

- `NODE_NAME`：必须，节点名，建议通过 Downward API 注入
- `KUBELET_HOST`、`KUBELET_PORT`：kubelet API 地址和端口，默认 localhost:10250
- `KUBELET_SKIP_VERIFY`：本地调试可设为 true 跳过 TLS 校验
- `KUBELET_CA_PATH`：kubelet API CA 证书路径（PEM，可选）
- `KUBELET_CLIENT_CERT_PATH`/`KUBELET_CLIENT_KEY_PATH`：客户端证书/私钥（PEM，可选）
- `KUBELET_TOKEN_PATH`：kubelet API Token 路径（本地调试可选，若不设置则不加 Authorization header）

### 2. DaemonSet 注入节点名示例

```yaml
env:
  - name: NODE_NAME
    valueFrom:
      fieldRef:
        fieldPath: spec.nodeName
```

### 3. 本地调试典型用法

- 若本地无 Token，可不设置 `KUBELET_TOKEN_PATH`，kubelet 需允许匿名访问或配置合适的 RBAC。
- 如需安全访问本地 kubelet，可指定 CA、客户端证书、私钥：

```yaml
env:
  - name: KUBELET_CA_PATH
    value: "/etc/kubernetes/pki/kubelet-ca.pem"
  - name: KUBELET_CLIENT_CERT_PATH
    value: "/etc/kubernetes/pki/client.pem"
  - name: KUBELET_CLIENT_KEY_PATH
    value: "/etc/kubernetes/pki/client-key.pem"
  - name: KUBELET_TOKEN_PATH
    value: "/tmp/my-debug-token"
  - name: KUBELET_SKIP_VERIFY
    value: "true"
```

- 推荐本地开发时用 `KUBELET_SKIP_VERIFY=true`，生产环境建议配置 CA 证书和 Token。

### 4. 典型调试场景

- **本地无Token，跳过认证**：
  - 只需设置 `KUBELET_HOST`、`KUBELET_PORT`、`KUBELET_SKIP_VERIFY=true`，不设置 `KUBELET_TOKEN_PATH`。
- **本地有自定义Token**：
  - 设置 `KUBELET_TOKEN_PATH` 指向本地 Token 文件。
- **本地自签名证书**：
  - 设置 `KUBELET_CA_PATH`，如有双向认证再加 `KUBELET_CLIENT_CERT_PATH` 和 `KUBELET_CLIENT_KEY_PATH`。

---

## 变更历史

[CHANGELOG.md](./docs/CHANGELOG.md)
