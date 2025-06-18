# Kubernetes NVMe 磁盘 IOPS 限速服务 - Go 版本

## 项目概述

本项目将原始的 bash 脚本自动化方案转换为 Go 语言实现的主进程服务，提供了更好的性能、可靠性和可维护性。

## 主要改进

### 1. 从脚本到服务的转变

**原始方案（bash 脚本）：**
- 定时轮询容器列表
- 依赖外部命令（docker、ctr、lsblk 等）
- 单次执行，需要循环调用
- 错误处理有限

**Go 服务方案：**
- 实时事件监听（Docker API、containerd events）
- 原生 SDK 调用，减少外部依赖
- 持续运行的主进程服务
- 完善的错误处理和重试机制

### 2. 技术架构改进

#### 自动检测机制
```go
// 自动检测容器运行时
func detectRuntime() string {
    if _, err := exec.LookPath("docker"); err == nil {
        return "docker"
    }
    if _, err := exec.LookPath("ctr"); err == nil {
        return "containerd"
    }
    return "none"
}

// 自动检测 cgroup 版本
func detectCgroupVersion() string {
    if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err == nil {
        return "v2"
    }
    return "v1"
}
```

#### 事件驱动架构
```go
// Docker 事件监听
func (d *DockerRuntime) watchContainerEvents() error {
    events, errs := d.client.Events(context.Background(), types.EventsOptions{
        Filters: map[string][]string{
            "type":  {"container"},
            "event": {"create"},
        },
    })
    
    for {
        select {
        case event := <-events:
            if event.Type == "container" && event.Action == "create" {
                // 处理新容器创建事件
            }
        case err := <-errs:
            // 错误处理和重连
        }
    }
}
```

#### 智能容器过滤
```go
func shouldSkip(container *ContainerInfo, excludeKeywords []string) bool {
    for _, keyword := range excludeKeywords {
        if strings.Contains(container.Image, keyword) || 
           strings.Contains(container.Name, keyword) {
            return true
        }
    }
    return false
}
```

### 3. 配置管理

#### 环境变量配置
```go
type Config struct {
    ContainerIOPSLimit    int      `json:"container_iops_limit"`
    DataTotalIOPS         int      `json:"data_total_iops"`
    DataMount             string   `json:"data_mount"`
    ExcludeKeywords       []string `json:"exclude_keywords"`
    ContainerdNamespace   string   `json:"containerd_namespace"`
    ContainerRuntime      string   `json:"container_runtime"`
    CgroupVersion         string   `json:"cgroup_version"`
    CheckInterval         int      `json:"check_interval"`
}
```

#### 默认配置
```go
func getDefaultConfig() *Config {
    return &Config{
        ContainerIOPSLimit:    500,
        DataTotalIOPS:         3000,
        DataMount:             "/data",
        ExcludeKeywords:       []string{"pause", "istio-proxy", "psmdb", "kube-system", "koordinator", "apisix"},
        ContainerdNamespace:   "k8s.io",
        ContainerRuntime:      "auto",
        CgroupVersion:         "auto",
        CheckInterval:         30,
    }
}
```

### 4. 运行时适配

#### Docker 运行时
- 使用 Docker Go SDK
- 实时事件监听
- 容器信息获取
- cgroup 路径构建

#### Containerd 运行时
- 使用 ctr 命令接口
- 事件流解析
- 容器信息解析
- cgroup 路径查找

### 5. cgroup 版本支持

#### cgroup v1
```go
// 写入 blkio.throttle 文件
readFile := filepath.Join(cgroupPath, "blkio.throttle.read_iops_device")
writeFile := filepath.Join(cgroupPath, "blkio.throttle.write_iops_device")

os.WriteFile(readFile, []byte(majMin+" "+iopsLimit), 0644)
os.WriteFile(writeFile, []byte(majMin+" "+iopsLimit), 0644)
```

#### cgroup v2
```go
// 写入 io.max 文件
ioMaxFile := filepath.Join(cgroupPath, "io.max")
content := fmt.Sprintf("%s riops=%s wiops=%s", majMin, iopsLimit, iopsLimit)
os.WriteFile(ioMaxFile, []byte(content), 0644)
```

## 部署方案

### 1. Docker 镜像构建
```dockerfile
# 多阶段构建
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o iops-limit-service .

FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/iops-limit-service .
CMD ["./iops-limit-service"]
```

### 2. Kubernetes DaemonSet
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: iops-limit-service
  namespace: kube-system
spec:
  template:
    spec:
      hostPID: true
      containers:
      - name: iops-limit-service
        image: your-registry/iops-limit-service:latest
        securityContext:
          privileged: true
        env:
        - name: CONTAINER_IOPS_LIMIT
          value: "500"
        - name: DATA_MOUNT
          value: "/data"
        volumeMounts:
        - name: cgroup
          mountPath: /sys/fs/cgroup
        - name: docker-socket
          mountPath: /var/run/docker.sock
```

### 3. 自动化部署脚本
```bash
#!/bin/bash
# 支持构建、推送、部署、状态查看等操作
./scripts/deploy.sh --build --push --deploy
./scripts/deploy.sh --status
./scripts/deploy.sh --logs
```

## 性能优势

### 1. 资源使用
- **内存占用**：Go 服务约 10-20MB，比 bash 脚本更高效
- **CPU 使用**：事件驱动，减少不必要的轮询
- **启动时间**：快速启动，无需等待外部命令

### 2. 响应速度
- **实时响应**：容器创建后立即应用限制
- **事件驱动**：无需轮询，减少延迟
- **并发处理**：支持多个容器同时创建

### 3. 可靠性
- **错误恢复**：自动重连和重试机制
- **日志记录**：详细的日志输出
- **健康检查**：Kubernetes 探针支持

## 监控和运维

### 1. 日志输出
```go
log.Printf("[%s] Set IOPS limit at %s: %s %s (v1)", 
    time.Now().Format("2006-01-02 15:04:05"), 
    cgroupPath, majMin, iopsLimit)
```

### 2. 健康检查
```yaml
livenessProbe:
  exec:
    command:
    - /bin/sh
    - -c
    - "ps aux | grep iops-limit-service | grep -v grep"
readinessProbe:
  exec:
    command:
    - /bin/sh
    - -c
    - "test -f /proc/1/root/app/iops-limit-service"
```

### 3. 资源限制
```yaml
resources:
  requests:
    memory: "64Mi"
    cpu: "50m"
  limits:
    memory: "128Mi"
    cpu: "100m"
```

## 测试验证

### 1. 单元测试
```go
func TestShouldSkip(t *testing.T) {
    tests := []struct {
        name     string
        image    string
        containerName string
        keywords []string
        expected bool
    }{
        // 测试用例
    }
    // 测试逻辑
}
```

### 2. 集成测试
```bash
# 创建测试容器
docker run -d --name test-container -v /data:/data alpine sleep 3600

# 验证 IOPS 限制
fio --name=test-iops --directory=/data --rw=randrw --bs=4k --size=100M --numjobs=1 --iodepth=1 --runtime=60 --time_based --group_reporting --direct=1
```

## 使用场景

### 1. 生产环境
- Kubernetes 集群中的 NVMe 磁盘 IOPS 隔离
- 多租户环境下的资源限制
- 高密度容器部署的性能保障

### 2. 开发环境
- 本地开发时的资源限制
- 测试环境的性能模拟
- CI/CD 流水线中的资源控制

## 总结

Go 版本的 IOPS 限速服务相比原始的 bash 脚本方案具有以下优势：

1. **更好的性能**：事件驱动，减少资源消耗
2. **更高的可靠性**：完善的错误处理和重试机制
3. **更强的可维护性**：结构化代码，易于扩展
4. **更好的监控**：详细的日志和健康检查
5. **更灵活的配置**：环境变量配置，支持动态调整
6. **更广泛的兼容性**：支持 Docker 和 containerd，cgroup v1 和 v2

这个 Go 服务为 Kubernetes 环境中的 NVMe 磁盘 IOPS 管理提供了一个现代化、可靠的解决方案。 