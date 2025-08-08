# KubeDiskGuard Runtime 集成方案（Containerd & Docker）

## 概述

本文档详细说明了 KubeDiskGuard 如何在 Runtime 层直接通过容器运行时 API（Containerd 和 Docker）获取容器的 cgroup 路径，实现更准确的磁盘 I/O 限速控制。

## 背景

历史版本的 cgroup 路径查找方式依赖于模式匹配和文件系统遍历，存在以下问题：

1. **不够精确**：模式匹配可能匹配到错误的路径
2. **性能开销**：文件系统遍历耗时较长
3. **兼容性问题**：不同容器运行时的路径格式差异
4. **维护困难**：需要维护多种路径模式

## 架构设计

### 新架构（Runtime 层直接获取）

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Runtime       │    │   Containerd     │    │   Container     │
│   Manager       │───▶│   API Client     │───▶│   Spec          │
└─────────────────┘    └──────────────────┘    └─────────────────┘
         │                       │                       │
         │                       │                       ▼
         │                       │              ┌─────────────────┐
         │                       │              │   CgroupsPath   │
         │                       │              └─────────────────┘
         │                       │                       │
         │                       │                       ▼
         │                       │              ┌─────────────────┐
         │                       │              │   完整 Cgroup   │
         │                       │              │   路径构建      │
         │                       │              └─────────────────┘
         │                                               │
         ▼                                               ▼
┌─────────────────┐                            ┌──────────────────┐
│   SetLimits     │───────────────────────────▶│   Cgroup         │
│   ResetLimits   │                            │   Operations     │
└─────────────────┘                            └──────────────────┘
```

### 核心改进

1. **Runtime 层负责路径获取**：直接在 `ContainerdRuntime` 中获取 cgroup 路径
2. **简化 Cgroup Manager**：只负责基本的 cgroup 操作，不再处理路径查找
3. **精确路径定位**：通过 containerd API 直接获取容器规格中的 `CgroupsPath`
4. **版本兼容**：根据 cgroup 版本自动构建正确的完整路径

## 实现细节

### 1. ContainerdRuntime 结构

```go
type ContainerdRuntime struct {
    config *config.Config
    cgroup *cgroup.Manager  // 简化的 cgroup manager
    client *containerd.Client
}
```

### 2. 核心方法实现

#### Containerd Runtime - getCgroupPath 方法

```go
func (c *ContainerdRuntime) getCgroupPath(containerID string) (string, error) {
    ctx := namespaces.WithNamespace(context.Background(), c.config.ContainerdNamespace)
    
    // 获取容器信息
    cont, err := c.client.LoadContainer(ctx, containerID)
    if err != nil {
        return "", fmt.Errorf("failed to load container: %v", err)
    }
    
    // 获取容器规格信息
    spec, err := cont.Spec(ctx)
    if err != nil {
        return "", fmt.Errorf("failed to get container spec: %v", err)
    }
    
    // 从规格中获取 cgroup 路径
    if spec.Linux == nil || spec.Linux.CgroupsPath == "" {
        return "", fmt.Errorf("no cgroup path found in container spec")
    }
    
    cgroupsPath := spec.Linux.CgroupsPath
    
    // 根据 cgroup 版本和 systemd 管理模式构建完整路径
    if c.config.CgroupVersion == "v1" {
        // cgroup v1: 需要指定子系统路径
        return fmt.Sprintf("/sys/fs/cgroup/blkio%s", cgroupsPath), nil
    } else {
        // cgroup v2: 检查是否为 systemd 管理的 cgroup 路径格式
        if c.isSystemdCgroupPath(cgroupsPath) {
            // systemd 管理模式: 转换路径格式
            return c.convertSystemdCgroupPath(cgroupsPath)
        } else {
            // 非 systemd 管理模式: 直接拼接路径
            if strings.HasPrefix(cgroupsPath, "/") {
                return fmt.Sprintf("/sys/fs/cgroup%s", cgroupsPath), nil
            } else {
                return fmt.Sprintf("/sys/fs/cgroup/%s", cgroupsPath), nil
            }
        }
    }
}
```

#### Systemd Cgroup 路径处理

新增了对 systemd 管理的 cgroup 路径的支持，包含以下辅助方法：

**isSystemdCgroupPath 方法**：检查是否为 systemd 管理的 cgroup 路径格式
```go
func (c *ContainerdRuntime) isSystemdCgroupPath(cgroupsPath string) bool {
    return strings.Contains(cgroupsPath, ".slice:") && strings.Contains(cgroupsPath, "cri-containerd")
}
```

**convertSystemdCgroupPath 方法**：转换 systemd cgroup 路径为实际文件系统路径
```go
func (c *ContainerdRuntime) convertSystemdCgroupPath(cgroupsPath string) (string, error) {
    // 输入格式: kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice:cri-containerd:16c0f5cee8ed9
    // 输出格式: /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-besteffort.slice/kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice/cri-containerd-16c0f5cee8ed9.scope/
    
    parts := strings.Split(cgroupsPath, ":")
    if len(parts) != 3 {
        return "", fmt.Errorf("invalid systemd cgroup path format: %s", cgroupsPath)
    }
    
    slicePath := parts[0]   // kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice
    service := parts[1]     // cri-containerd
    containerID := parts[2] // 16c0f5cee8ed9
    
    // 移除末尾的.slice后缀来获取纯净的slice名称
    sliceNameWithoutSuffix := strings.TrimSuffix(slicePath, ".slice")
    
    // 构建slice层次结构
    sliceComponents := strings.Split(sliceNameWithoutSuffix, "-")
    var pathComponents []string
    
    // 添加根slice
    pathComponents = append(pathComponents, "kubelet.slice")
    
    // 构建中间slice路径
    currentSlice := "kubelet"
    for i := 1; i < len(sliceComponents); i++ {
        currentSlice += "-" + sliceComponents[i]
        pathComponents = append(pathComponents, currentSlice+".slice")
    }
    
    // 添加最终的scope
    scopeName := fmt.Sprintf("%s-%s.scope", service, containerID)
    pathComponents = append(pathComponents, scopeName)
    
    // 构建完整路径
    fullPath := "/sys/fs/cgroup/" + strings.Join(pathComponents, "/") + "/"
    return fullPath, nil
}
```

#### Docker Runtime - getCgroupPath 方法

```go
func (d *DockerRuntime) getCgroupPath(containerID string) (string, error) {
    ctx := context.Background()
    
    // 获取容器详细信息
    info, err := d.client.ContainerInspect(ctx, containerID)
    if err != nil {
        return "", fmt.Errorf("failed to inspect container: %v", err)
    }
    
    // 从容器信息中获取 cgroup 路径
    // Docker 容器的 cgroup 路径通常在 HostConfig.CgroupParent 或者可以从容器 ID 构建
    var cgroupsPath string
    if info.HostConfig.CgroupParent != "" {
        // 如果有明确的 cgroup parent，使用它构建完整路径
        // 格式: {CgroupParent}/{containerID}
        cgroupsPath = fmt.Sprintf("%s/%s", info.HostConfig.CgroupParent, containerID)
    } else {
        // 默认的 Docker cgroup 路径格式
        if d.config.CgroupVersion == "v1" {
            cgroupsPath = fmt.Sprintf("/docker/%s", containerID)
        } else {
            // cgroup v2 的路径格式
            cgroupsPath = fmt.Sprintf("/system.slice/docker-%s.scope", containerID)
        }
    }
    
    // 根据 cgroup 版本构建完整路径
    if d.config.CgroupVersion == "v1" {
        // cgroup v1: 需要指定子系统路径
        // 实际路径格式: /sys/fs/cgroup/blkio/{CgroupParent}/{containerID}
        return fmt.Sprintf("/sys/fs/cgroup/blkio%s", cgroupsPath), nil
    } else {
        // cgroup v2: 统一层次结构
        return fmt.Sprintf("/sys/fs/cgroup%s", cgroupsPath), nil
    }
}
```

该方法通过 Docker API 获取容器的详细信息，并根据 cgroup 版本和容器信息构建 cgroup 路径。

**Docker Cgroup v1 路径示例：**
```bash
# Docker inspect 获取 CgroupParent
$ docker inspect b6abba6fc2318 | jq .[].HostConfig.CgroupParent
"/kubepods/burstable/pod4b5860c3-7ac8-47b6-921d-053136bfc3c9"

# 实际文件系统路径
$ ls -l /sys/fs/cgroup/blkio/kubepods/burstable/pod4b5860c3-7ac8-47b6-921d-053136bfc3c9/
drwxr-xr-x 2 root root 0 Jul 23  2024 b6abba6fc231831d331f08ced6d004c94996e184761018fed9514c37cf8e97a5
-rw-r--r-- 1 root root 0 Aug  8 12:17 blkio.throttle.read_bps_device
-rw-r--r-- 1 root root 0 Aug  8 12:17 blkio.throttle.write_bps_device
```

#### SetLimits 和 ResetLimits 方法

```go
func (c *ContainerdRuntime) SetLimits(container *container.ContainerInfo, riops, wiops, rbps, wbps int) error {
    majMin, err := device.GetMajMin(c.config.DataMount)
    if err != nil {
        return err
    }
    cgroupPath, err := c.getCgroupPath(container.ID)
    if err != nil {
        return fmt.Errorf("failed to get cgroup path for container %s: %v", container.ID, err)
    }
    return c.cgroup.SetLimits(cgroupPath, majMin, riops, wiops, rbps, wbps)
}

func (c *ContainerdRuntime) ResetLimits(container *container.ContainerInfo) error {
    majMin, err := device.GetMajMin(c.config.DataMount)
    if err != nil {
        return err
    }
    cgroupPath, err := c.getCgroupPath(container.ID)
    if err != nil {
        return fmt.Errorf("failed to get cgroup path for container %s: %v", container.ID, err)
    }
    return c.cgroup.ResetLimits(cgroupPath, majMin)
}
```

### 3. 简化的 Cgroup Manager

```go
type Manager struct {
    version string
}

func NewManager(version string) *Manager {
    return &Manager{
        version: version,
    }
}
```

## 使用方法

### 1. 配置要求

#### Containerd 运行时配置

```json
{
    "container_runtime": "containerd",
    "container_socket_path": "/run/containerd/containerd.sock",
    "containerd_namespace": "k8s.io",
    "cgroup_version": "v1"
}
```

#### Docker 运行时配置

```json
{
    "container_runtime": "docker",
    "container_socket_path": "/var/run/docker.sock",
    "cgroup_version": "v1"
}
```

### 2. 运行时创建

#### Containerd 运行时

```go
runtime, err := runtime.NewContainerdRuntime(config)
if err != nil {
    log.Fatalf("Failed to create containerd runtime: %v", err)
}
defer runtime.Close()
```

#### Docker 运行时

```go
runtime, err := runtime.NewDockerRuntime(config)
if err != nil {
    log.Fatalf("Failed to create docker runtime: %v", err)
}
defer runtime.Close()
```

### 3. 限速操作

```go
// 设置限速
err := runtime.SetLimits(containerInfo, 100, 100, 1024*1024, 1024*1024)
if err != nil {
    log.Printf("Failed to set limits: %v", err)
}

// 解除限速
err = runtime.ResetLimits(containerInfo)
if err != nil {
    log.Printf("Failed to reset limits: %v", err)
}
```

## 优势

### 1. 精确性
- **直接获取**：通过 containerd API 直接获取容器规格中的 cgroup 路径
- **无歧义**：避免模式匹配可能产生的错误匹配
- **实时准确**：获取的是容器当前实际使用的 cgroup 路径
- **Systemd 兼容**：完整支持 systemd 管理的 cgroup 路径格式

### 2. 性能
- **高效获取**：直接 API 调用，无需文件系统遍历
- **缓存友好**：containerd 客户端内部有缓存机制
- **减少 I/O**：避免大量文件系统操作

### 3. 可维护性
- **简化逻辑**：移除复杂的模式匹配和回退机制
- **清晰分层**：Runtime 层负责路径获取，Cgroup 层负责操作
- **易于扩展**：新增容器运行时支持更容易

### 4. 兼容性
- **版本适配**：自动根据 cgroup 版本构建正确路径
- **命名空间支持**：支持不同的 containerd 命名空间
- **标准接口**：使用标准的 containerd Go API

## Cgroup v2 路径格式说明

### 非 Systemd 管理模式
当 cgroup v2 未启用 systemd 管理时，容器的 cgroup 路径格式如下：

**crictl inspect 输出示例**：
```json
{
  "cgroupsPath": "/kubepods/burstable/podc9c501eb-9423-4bd6-b96f-7f10f7f4527c/f3ee04629f75567e95fae8425cb3e9b3e1c91346b1a2ddee9139c9216c713dc7"
}
```

**实际文件系统路径**：
```
/sys/fs/cgroup/kubepods/burstable/podc9c501eb-9423-4bd6-b96f-7f10f7f4527c/f3ee04629f75567e95fae8425cb3e9b3e1c91346b1a2ddee9139c9216c713dc7
```

### Systemd 管理模式
当 cgroup v2 启用 systemd 管理时，容器的 cgroup 路径格式会根据 Kubernetes QoS 类别有所不同：

#### 1. BestEffort Pod（最低优先级）
**crictl inspect 输出示例**：
```json
{
  "systemd_cgroup": true,
  "cgroupsPath": "kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice:cri-containerd:16c0f5cee8ed9"
}
```

**实际文件系统路径**：
```
/sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-besteffort.slice/kubelet-kubepods-besteffort-podc42adbb2_915e_4883_aa26_7ed96c3196da.slice/cri-containerd-16c0f5cee8ed9.scope/
```

#### 2. Burstable Pod（可突发资源）
**crictl inspect 输出示例**：
```json
{
  "systemd_cgroup": true,
  "cgroupsPath": "kubelet-kubepods-burstable-pod123.slice:cri-containerd:abc123"
}
```

**实际文件系统路径**：
```
/sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-burstable.slice/kubelet-kubepods-burstable-pod123.slice/cri-containerd-abc123.scope/
```

#### 3. Guaranteed Pod（保证资源，直接 Pod Slice）
**crictl inspect 输出示例**：
```json
{
  "systemd_cgroup": true,
  "cgroupsPath": "kubelet-kubepods-poda5762175_5440_4e1e_be30_a69d9073ce0c.slice:cri-containerd:def456"
}
```

**实际文件系统路径**：
```
/sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/kubelet-kubepods-poda5762175_5440_4e1e_be30_a69d9073ce0c.slice/cri-containerd-def456.scope/
```

**文件系统结构验证**：
```bash
$ ls -l /sys/fs/cgroup/kubelet.slice/kubelet-kubepods.slice/
drwxr-xr-x 10 root root 0 Aug  4 07:21 kubelet-kubepods-besteffort.slice
drwxr-xr-x 10 root root 0 Aug  4 07:18 kubelet-kubepods-burstable.slice
drwxr-xr-x  4 root root 0 Aug  4 07:13 kubelet-kubepods-poda5762175_5440_4e1e_be30_a69d9073ce0c.slice
```

**路径转换规则**：
1. 解析 systemd cgroup 路径格式：`slice:service:containerID`
2. 构建 slice 层次结构：将 `-` 分隔的组件转换为嵌套的 `.slice` 目录
3. 智能识别 QoS 类型：自动处理 besteffort、burstable 和直接 pod slice
4. 添加最终的 scope：`service-containerID.scope`
![202508081234726](https://raw.githubusercontent.com/kbsonlong/notes_statics/master/images/202508081234726.png)
## 测试

### 1. 单元测试

```bash
# 测试 cgroup 基本功能
go test ./pkg/cgroup/ -v

# 测试完整项目
go test ./... -v
```

### 2. 集成测试

```bash
# 构建项目
go build -v ./...

# 运行 KubeDiskGuard
./KubeDiskGuard
```

## 故障排除

### 1. 常见问题

#### Containerd 连接失败
```
failed to connect to containerd: connection error
```

**解决方案**：
- 检查 containerd 是否运行：`systemctl status containerd`
- 检查 socket 路径是否正确：`ls -la /run/containerd/containerd.sock`
- 检查权限：确保进程有访问 socket 的权限

#### Docker 连接失败
```
failed to create docker client: connection error
```

**解决方案**：
- 检查 Docker 是否运行：`systemctl status docker`
- 检查 socket 路径是否正确：`ls -la /var/run/docker.sock`
- 检查权限：确保进程有访问 Docker socket 的权限

#### 容器规格获取失败
```
failed to get container spec: not found
```

**解决方案**：
- 检查容器 ID 是否正确
- 检查 containerd 命名空间配置
- 确认容器确实存在：`crictl ps`

#### cgroup 路径为空
```
no cgroup path found in container spec
```

**解决方案**：
- 检查容器是否正在运行
- 检查 containerd 配置是否正确
- 查看容器规格：`crictl inspect <container_id>`

### 2. 调试技巧

#### 启用详细日志
```go
log.SetLevel(log.DebugLevel)
```

#### 检查容器规格
```bash
crictl inspect <container_id> | jq '.info.config.linux.cgroupsPath'
```

#### 验证 cgroup 路径
```bash
ls -la /sys/fs/cgroup/blkio/<cgroup_path>
```

## 最佳实践

### 1. 配置管理
- 使用环境变量或配置文件管理 containerd 连接参数
- 定期检查 containerd 服务状态
- 监控 containerd API 调用的性能指标

### 2. 错误处理
- 实现重试机制处理临时网络问题
- 记录详细的错误日志便于排查
- 提供降级方案（如回退到模式匹配）

### 3. 性能优化
- 复用 containerd 客户端连接
- 实现适当的缓存机制
- 监控内存使用情况

### 4. 安全考虑
- 限制 containerd socket 的访问权限
- 使用最小权限原则
- 定期更新 containerd 客户端库

## 总结

通过将 cgroup 路径获取逻辑移到 Runtime 层，KubeDiskGuard 实现了：

1. **更高的精确性**：直接从容器运行时 API 获取 cgroup 路径
2. **更好的性能**：避免文件系统遍历和模式匹配
3. **更清晰的架构**：职责分离，代码更易维护
4. **更强的兼容性**：支持不同的 cgroup 版本和配置
5. **多运行时支持**：同时支持 Containerd 和 Docker 运行时

### 运行时特性对比

| 特性 | Containerd | Docker |
|------|------------|--------|
| API 获取路径 | ✅ 通过容器规格 | ✅ 通过容器检查 |
| Cgroup v1 支持 | ✅ | ✅ |
| Cgroup v2 支持 | ✅ | ✅ |
| 命名空间支持 | ✅ | ❌ |
| 性能 | 高 | 中等 |
| 路径精确性 | 极高 | 高 |

这种设计使得 KubeDiskGuard 能够更可靠地对 Kubernetes 集群中的容器进行磁盘 I/O 限速控制，无论使用哪种容器运行时。