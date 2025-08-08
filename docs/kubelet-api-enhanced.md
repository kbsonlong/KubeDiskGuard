# Enhanced Kubelet API 调用方式

本文档介绍了改进后的 kubelet API 调用方式，集成了 `examples/kubelet-api-client-go-example.go` 中的逻辑到 `pkg/kubeclient` 包中。

## 主要改进

### 1. 增强的认证支持

- **客户端证书认证**: 从 kubeconfig 中自动提取客户端证书和私钥
- **ServiceAccount Token 认证**: 支持自定义 token 路径和默认 SA token
- **CA 证书处理**: 自动从 kubeconfig 提取 CA 证书或使用指定路径
- **认证方式检测**: 自动检测并优先使用客户端证书认证

### 2. 改进的 TLS 配置

- **服务器名称验证**: 支持自定义 `ServerName` 配置
- **证书验证跳过**: 可选的 `InsecureSkipVerify` 配置
- **CA 证书加载**: 支持从文件或 kubeconfig 加载 CA 证书

### 3. 详细的日志输出

- **认证方式显示**: 清晰显示当前使用的认证方式
- **证书路径信息**: 显示客户端证书和 CA 证书路径
- **连接状态**: 提供连接测试和状态反馈

## 使用方式

### 基本用法

```go
package main

import (
    "KubeDiskGuard/pkg/config"
    "KubeDiskGuard/pkg/kubeclient"
)

func main() {
    // 创建配置
    cfg := &config.Config{
        KubeletHost:       "localhost",
        KubeletPort:       "10250",
        KubeletSkipVerify: false,
        KubeletServerName: "node-name",
    }
    
    // 创建客户端
    client, err := kubeclient.NewKubeClientWithConfig(
        "node-name", 
        "/path/to/kubeconfig", 
        cfg,
    )
    if err != nil {
        panic(err)
    }
    
    // 测试连接
    if err := client.TestKubeletConnection(); err != nil {
        panic(err)
    }
    
    // 获取节点摘要
    summary, err := client.GetNodeSummary()
    if err != nil {
        panic(err)
    }
    
    // 获取 cAdvisor 指标
    metrics, err := client.GetCadvisorMetrics()
    if err != nil {
        panic(err)
    }
}
```

### 环境变量配置

支持以下环境变量进行配置：

```bash
# 节点信息
export NODE_NAME="your-node-name"
export KUBECONFIG="/path/to/kubeconfig"

# kubelet 配置
export KUBELET_HOST="localhost"          # 默认 localhost
export KUBELET_PORT="10250"              # 默认 10250
export KUBELET_SKIP_VERIFY="false"       # 默认 false
export KUBELET_SERVER_NAME="node-name"   # 可选

# 认证配置（可选，优先从 kubeconfig 提取）
export KUBELET_CA_PATH="/path/to/ca.crt"
export KUBELET_TOKEN_PATH="/path/to/token"
```

### 测试程序

使用提供的测试程序验证功能：

```bash
# 设置环境变量
export NODE_NAME="your-node-name"
export KUBECONFIG="$HOME/.kube/config"

# 运行测试
go run cmd/test-kubelet-api-enhanced/main.go
```

## 认证方式优先级

1. **客户端证书认证** (最高优先级)
   - 从 kubeconfig 的 `client-certificate-data` 和 `client-key-data` 提取
   - 或使用 `client-certificate` 和 `client-key` 文件路径

2. **ServiceAccount Token 认证**
   - 使用 `KubeletTokenPath` 指定的 token 文件
   - 或使用默认的 `/var/run/secrets/kubernetes.io/serviceaccount/token`

3. **无认证**
   - 当没有配置任何认证方式时

## API 方法

### 连接测试

```go
// 测试 kubelet 连接
err := client.TestKubeletConnection()
```

### 节点信息

```go
// 获取节点摘要
summary, err := client.GetNodeSummary()

// 获取节点 Pod 列表（优先从 kubelet 获取）
pods, err := client.ListNodePodsWithKubeletFirst()
```

### 监控指标

```go
// 获取 cAdvisor 原始指标
metrics, err := client.GetCadvisorMetrics()

// 解析 cAdvisor 指标
parsedMetrics, err := client.ParseCadvisorMetrics(metrics)

// 获取容器 IO 速率
ioRate, err := client.GetCadvisorIORate(containerID, window)
```

## 错误处理

所有方法都返回详细的错误信息，包括：

- 连接错误
- 认证失败
- TLS 证书问题
- HTTP 状态码错误

```go
if err := client.TestKubeletConnection(); err != nil {
    if strings.Contains(err.Error(), "connection refused") {
        // 处理连接被拒绝
    } else if strings.Contains(err.Error(), "certificate") {
        // 处理证书问题
    }
    // 其他错误处理
}
```

## 安全注意事项

1. **证书验证**: 生产环境中避免使用 `InsecureSkipVerify: true`
2. **文件权限**: 确保证书和密钥文件有适当的权限（600 或 400）
3. **临时文件**: 从 kubeconfig 提取的证书会写入临时文件，程序退出时会自动清理
4. **敏感信息**: 避免在日志中输出完整的证书内容或 token

## 故障排除

### 常见问题

1. **连接被拒绝**
   ```
   connection refused
   ```
   - 检查 kubelet 是否运行在指定端口
   - 确认防火墙设置

2. **证书验证失败**
   ```
   certificate verify failed
   ```
   - 检查 CA 证书是否正确
   - 确认 ServerName 配置
   - 考虑使用 `InsecureSkipVerify: true` 进行测试

3. **认证失败**
   ```
   401 Unauthorized
   ```
   - 检查客户端证书是否有效
   - 确认 ServiceAccount token 是否正确
   - 验证 RBAC 权限配置

### 调试模式

程序会输出详细的认证和连接信息，包括：

- 使用的认证方式
- 证书文件路径
- TLS 配置参数
- HTTP 请求详情

这些信息有助于诊断连接问题。