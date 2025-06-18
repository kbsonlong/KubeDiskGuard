# 包结构说明

本项目采用模块化的包结构设计，将功能按照职责进行分离，提高了代码的可维护性和可扩展性。

## 目录结构

```
./
├── main.go                    # 主程序入口
├── main_test.go              # 主程序测试
├── go.mod                    # Go 模块定义
├── Dockerfile                # Docker 镜像构建
├── k8s-daemonset.yaml        # Kubernetes 部署配置
├── Makefile                  # 构建和部署脚本
├── README.md                 # 项目说明
├── PROJECT_SUMMARY.md        # 项目总结
├── PACKAGE_STRUCTURE.md      # 包结构说明（本文档）
├── .gitignore               # Git 忽略文件
├── scripts/
│   └── deploy.sh            # 自动化部署脚本
└── pkg/                     # 包目录
    ├── config/              # 配置管理包
    │   └── config.go
    ├── container/           # 容器相关包
    │   └── container.go
    ├── detector/            # 检测器包
    │   └── detector.go
    ├── cgroup/              # cgroup 管理包
    │   └── cgroup.go
    ├── device/              # 设备管理包
    │   └── device.go
    ├── runtime/             # 运行时包
    │   ├── docker.go        # Docker 运行时
    │   └── containerd.go    # containerd 运行时
    └── service/             # 服务包
        └── service.go
```

## 包详细说明

### 1. `pkg/config` - 配置管理包

**职责**：管理应用程序的配置信息

**主要功能**：
- 定义配置结构体
- 提供默认配置
- 从环境变量加载配置
- 配置序列化

**核心类型**：
```go
type Config struct {
    ContainerIOPSLimit  int      `json:"container_iops_limit"`
    DataTotalIOPS       int      `json:"data_total_iops"`
    DataMount           string   `json:"data_mount"`
    ExcludeKeywords     []string `json:"exclude_keywords"`
    ContainerdNamespace string   `json:"containerd_namespace"`
    ContainerRuntime    string   `json:"container_runtime"`
    CgroupVersion       string   `json:"cgroup_version"`
    CheckInterval       int      `json:"check_interval"`
}
```

**主要函数**：
- `GetDefaultConfig()` - 获取默认配置
- `LoadFromEnv(config *Config)` - 从环境变量加载配置
- `ToJSON()` - 将配置转换为 JSON 字符串

### 2. `pkg/container` - 容器相关包

**职责**：定义容器相关的数据结构和接口

**主要功能**：
- 定义容器信息结构体
- 定义运行时接口
- 提供容器过滤逻辑

**核心类型**：
```go
type ContainerInfo struct {
    ID           string
    Image        string
    Name         string
    CgroupParent string
}

type Runtime interface {
    GetContainers() ([]*ContainerInfo, error)
    GetContainerByID(containerID string) (*ContainerInfo, error)
    WatchContainerEvents() error
    ProcessContainer(container *ContainerInfo) error
}
```

**主要函数**：
- `ShouldSkip(container *ContainerInfo, excludeKeywords []string) bool` - 检查是否应该跳过容器

### 3. `pkg/detector` - 检测器包

**职责**：自动检测系统环境

**主要功能**：
- 检测容器运行时类型
- 检测 cgroup 版本

**主要函数**：
- `DetectRuntime() string` - 检测容器运行时
- `DetectCgroupVersion() string` - 检测 cgroup 版本

### 4. `pkg/cgroup` - cgroup 管理包

**职责**：管理 cgroup 相关操作

**主要功能**：
- 查找 cgroup 路径
- 设置 IOPS 限制
- 构建 cgroup 路径

**核心类型**：
```go
type Manager struct {
    version string
}
```

**主要方法**：
- `FindCgroupPath(containerID string) string` - 查找 cgroup 路径
- `SetIOPSLimit(cgroupPath, majMin string, iopsLimit int) error` - 设置 IOPS 限制
- `BuildCgroupPath(containerID, cgroupParent string) string` - 构建 cgroup 路径

### 5. `pkg/device` - 设备管理包

**职责**：管理设备相关操作

**主要功能**：
- 获取设备主次设备号

**主要函数**：
- `GetMajMin(dataMount string) (string, error)` - 获取设备主次设备号

### 6. `pkg/runtime` - 运行时包

**职责**：实现不同容器运行时的具体逻辑

#### 6.1 `pkg/runtime/docker.go` - Docker 运行时

**职责**：实现 Docker 运行时的具体逻辑

**主要功能**：
- 使用 Docker API 获取容器信息
- 监听 Docker 容器事件
- 处理 Docker 容器

**核心类型**：
```go
type DockerRuntime struct {
    client *client.Client
    config *config.Config
    cgroup *cgroup.Manager
}
```

#### 6.2 `pkg/runtime/containerd.go` - containerd 运行时

**职责**：实现 containerd 运行时的具体逻辑

**主要功能**：
- 使用 ctr 命令获取容器信息
- 监听 containerd 容器事件
- 处理 containerd 容器

**核心类型**：
```go
type ContainerdRuntime struct {
    config *config.Config
    cgroup *cgroup.Manager
}
```

### 7. `pkg/service` - 服务包

**职责**：整合各个包，提供完整的服务功能

**主要功能**：
- 初始化服务
- 处理现有容器
- 监听容器事件
- 运行服务

**核心类型**：
```go
type IOPSLimitService struct {
    config  *config.Config
    runtime container.Runtime
}
```

**主要方法**：
- `NewIOPSLimitService(config *config.Config) (*IOPSLimitService, error)` - 创建服务实例
- `ProcessExistingContainers() error` - 处理现有容器
- `WatchEvents() error` - 监听事件
- `Run() error` - 运行服务

## 设计原则

### 1. 单一职责原则
每个包都有明确的职责，只负责特定的功能领域。

### 2. 依赖倒置原则
通过接口定义依赖关系，而不是依赖具体实现。

### 3. 开闭原则
对扩展开放，对修改关闭。可以通过实现新的运行时来支持其他容器运行时。

### 4. 接口隔离原则
接口定义简洁，只包含必要的方法。

## 扩展性

### 添加新的容器运行时
1. 在 `pkg/runtime` 目录下创建新的运行时文件
2. 实现 `container.Runtime` 接口
3. 在 `pkg/service` 中添加对新运行时的支持

### 添加新的配置项
1. 在 `pkg/config/config.go` 中添加新的配置字段
2. 在 `LoadFromEnv` 函数中添加环境变量读取逻辑
3. 在相关包中使用新的配置项

### 添加新的 cgroup 版本支持
1. 在 `pkg/cgroup/cgroup.go` 中添加新版本的处理逻辑
2. 在 `pkg/detector/detector.go` 中添加新版本的检测逻辑

## 测试策略

每个包都有对应的测试文件，测试覆盖：
- 单元测试：测试包内的函数和方法
- 集成测试：测试包之间的交互
- 基准测试：测试性能关键路径

## 依赖关系

```
main.go
  ↓
pkg/service
  ↓
pkg/config + pkg/container + pkg/detector + pkg/runtime
  ↓
pkg/cgroup + pkg/device
```

这种包结构设计使得代码：
- **模块化**：每个包都有明确的职责
- **可测试**：每个包都可以独立测试
- **可扩展**：容易添加新功能
- **可维护**：代码结构清晰，易于理解和修改 