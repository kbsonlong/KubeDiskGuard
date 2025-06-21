<p align="center">
  <img src="./logo.svg" width="120" alt="KubeDiskGuard Logo"/>
</p>

<h1 align="center">KubeDiskGuard</h1>
<p align="center">Kubernetes 节点级磁盘 IO 资源守护与限速服务</p> 

---

# 开发手册（Developer Guide）

## 开发环境准备

### 1. Go 版本与依赖
- 推荐 Go 1.18 及以上版本
- 安装依赖：
  ```bash
  go mod download
  ```
- 如需指定代理：
  ```bash
  export GOPROXY=https://goproxy.cn,direct
  ```

### 2. 本地环境变量配置
- 推荐在 `.env` 或 shell 环境中设置，常用变量如下：
  ```bash
  export NODE_NAME=dev-node
  export DATA_MOUNT=/data
  export CONTAINER_READ_IOPS_LIMIT=500
  export CONTAINER_WRITE_IOPS_LIMIT=500
  export CONTAINER_IOPS_LIMIT=500
  export KUBELET_HOST=localhost
  export KUBELET_PORT=10250
  export KUBELET_SKIP_VERIFY=true
  # 如需本地kubelet认证
  export KUBELET_CA_PATH=/etc/kubernetes/pki/kubelet-ca.pem
  export KUBELET_CLIENT_CERT_PATH=/etc/kubernetes/pki/client.pem
  export KUBELET_CLIENT_KEY_PATH=/etc/kubernetes/pki/client-key.pem
  export KUBELET_TOKEN_PATH=/tmp/my-debug-token
  export KUBECONFIG_PATH=~/.kube/config
  export CONTAINER_SOCKET_PATH=/var/run/docker.sock
  export EXCLUDE_KEYWORDS="pause,istio-proxy,apisix"
  export EXCLUDE_NAMESPACES="kube-system,kruise-system,psmdb,istio-system,koordinator-system,kyverno"
  ```

### 3. 常用调试命令
- 本地运行主程序：
  ```bash
  go run main.go
  ```
- 构建二进制：
  ```bash
  go build -o io-limit-service main.go
  ```
- 运行全部单元测试：
  ```bash
  go test -v ./...
  ```
- 查看详细日志：
  - 默认日志输出到标准输出，可用 `kubectl logs` 或本地终端查看

### 4. 本地对接K8s/kubelet
- 推荐本地开发时用 `KUBELET_SKIP_VERIFY=true`，如需安全访问本地 kubelet，可指定 CA、客户端证书、私钥
- 若本地无 Token，可不设置 `KUBELET_TOKEN_PATH`，kubelet 需允许匿名访问或配置合适的 RBAC
- 典型用法见[用户手册](./USER_GUIDE.md)

### 5. 典型问题排查
- **Pod信息获取失败**：检查kubelet API地址、端口、证书、Token、网络连通性
- **限速未生效**：检查注解/环境变量、cgroup路径、挂载点、日志输出
- **依赖未安装**：执行 `go mod download` 并检查Go版本
- **日志无输出**：确认日志级别、终端/容器标准输出配置

---

## 项目结构
- `main.go`：程序入口，服务启动、参数解析
- `pkg/service/`：业务主流程，Pod事件监听、注解解析、限速下发
- `pkg/runtime/`：容器运行时适配（Docker、containerd），只做单容器操作
- `pkg/cgroup/`：cgroup v1/v2限速实现，统一SetLimits/ResetLimits接口
- `pkg/config/`：配置加载与环境变量解析
- `pkg/kubeclient/`：K8s API/kubelet API适配
- `pkg/device/`：设备号获取与挂载点处理

## 关键接口与主流程
- `container.Runtime`接口：只保留`GetContainerByID`、`SetLimits`、`ResetLimits`等单容器操作
- `service层`：以Pod为主索引，监听事件，解析注解，统一下发/解除限速
- `cgroup.Manager`：v1分文件写，v2一次性写io.max，支持IOPS/BPS统一设置

## 注解与环境变量解析
- 注解优先级：`read-iops`/`write-iops` > `iops`，`read-bps`/`write-bps` > `bps`
- 注解为0自动解除限速
- 环境变量作为全局默认值
- 解析逻辑详见`service.go`的`ParseIopsLimitFromAnnotations`、`ParseBpsLimitFromAnnotations`

## 事件监听与主流程
- 通过client-go监听Pod事件，优先kubelet API，fallback到API Server
- 只处理Running且所有业务容器Started为true的Pod
- 注解变更、服务重启等场景均以Pod为主索引，保证状态一致性

## 单元测试与mock
- 单元测试覆盖核心分支，mock runtime/kubeclient隔离业务逻辑
- 详见`main_test.go`和`pkg/service/service_test.go`
- 推荐新增功能时先补充测试用例

## 扩展开发建议
- 新增运行时：实现`container.Runtime`接口，注册到service层
- 新增指标采集/监控：可在service层埋点，或集成Prometheus等
- 支持多数据盘：扩展`device`和`cgroup`相关逻辑
- 代码风格建议：保持接口简洁、职责单一，注释完善

---
如需贡献代码，请先阅读本手册并参考[CHANGELOG.md](./CHANGELOG.md)。 