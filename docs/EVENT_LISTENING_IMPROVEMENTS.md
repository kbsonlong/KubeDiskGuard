# 事件监听改进说明

## 问题描述

原始的事件监听机制监听容器创建事件（`container create`），但在容器创建时可能还没有完全初始化，导致IOPS限制设置失败或不稳定。

## 解决方案

### 1. 事件类型调整

**之前：** 监听容器创建事件
- Docker: `event=create`
- Containerd: `containerd.events.ContainerCreate`

**现在：** 监听容器启动事件
- Docker: `event=start`
- Containerd: `containerd.events.TaskStart`

### 2. 延迟处理机制

在检测到容器启动事件后，添加2秒延迟，确保容器完全启动后再进行IOPS限制设置：

```go
// 等待一小段时间确保容器完全启动
time.Sleep(2 * time.Second)
```

### 3. 具体改进

#### Docker运行时 (`pkg/runtime/docker.go`)

```go
// 之前
f.Add("event", "create")

// 现在
f.Add("event", "start")
```

#### Containerd运行时 (`pkg/runtime/containerd.go`)

```go
// 之前
eventsCh, errCh := eventService.Subscribe(ctx, "type==\"container\"")
// 监听 ContainerCreate 事件

// 现在
eventsCh, errCh := eventService.Subscribe(ctx, "type==\"task\"")
// 监听 TaskStart 事件
```

## 优势

1. **更稳定的时机：** 容器启动时比创建时更稳定，cgroup路径和容器信息更完整
2. **避免竞态条件：** 减少因容器初始化不完整导致的IOPS设置失败
3. **更好的兼容性：** 适用于不同的容器运行时和Kubernetes版本

## 测试验证

运行测试验证事件监听功能：

```bash
go test -v -run TestEventListening
```

## 使用建议

1. 在生产环境中部署时，建议先在小范围测试验证效果
2. 可以根据实际环境调整延迟时间（当前为2秒）
3. 监控日志中的"Container started"消息，确认事件监听正常工作

## 日志示例

```
2025/06/18 18:10:28 Using container runtime: docker
2025/06/18 18:10:28 Detected cgroup version: v1
2025/06/18 18:10:28 Container started: abc123 (myapp)
2025/06/18 18:10:30 Successfully set IOPS limit for container abc123: 259:0 500
``` 