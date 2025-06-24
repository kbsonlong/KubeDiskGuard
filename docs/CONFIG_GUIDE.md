# KubeDiskGuard 配置指南

本文档详细说明了 KubeDiskGuard 的所有配置选项及其使用方法。

## 配置文件

项目提供了两种格式的默认配置文件：

- `config.default.yaml` - YAML格式默认配置
- `config.default.json` - JSON格式默认配置
- `config.example.yaml` - 示例配置文件（用于动态配置演示）

## 配置优先级

配置加载遵循以下优先级顺序：

1. **环境变量** (最高优先级)
2. **配置文件** (通过 `CONFIG_FILE_PATH` 环境变量指定)
3. **默认值** (最低优先级)

## 配置项详解

### 1. 容器IO限制配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `container_iops_limit` | int | 500 | 容器总IOPS限制 |
| `container_read_iops_limit` | int | 500 | 容器读IOPS限制 |
| `container_write_iops_limit` | int | 500 | 容器写IOPS限制 |
| `container_read_bps_limit` | int | 0 | 容器读带宽限制(字节/秒，0=不限制) |
| `container_write_bps_limit` | int | 0 | 容器写带宽限制(字节/秒，0=不限制) |

### 2. 基础配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `data_mount` | string | "/data" | 数据挂载点路径 |
| `exclude_keywords` | []string | ["pause", "istio-proxy", ...] | 排除的容器关键词 |
| `exclude_namespaces` | []string | ["kube-system"] | 排除的命名空间 |
| `exclude_label_selector` | string | "" | 排除的标签选择器 |

### 3. 容器运行时配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `containerd_namespace` | string | "k8s.io" | Containerd命名空间 |
| `container_runtime` | string | "auto" | 容器运行时(auto/docker/containerd) |
| `cgroup_version` | string | "auto" | Cgroup版本(auto/v1/v2) |
| `container_socket_path` | string | "/run/containerd/containerd.sock" | 容器运行时Socket路径 |

### 4. Kubelet API配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `kubelet_host` | string | "localhost" | Kubelet主机地址 |
| `kubelet_port` | string | "10250" | Kubelet端口 |
| `kube_config_path` | string | "" | Kubeconfig文件路径 |
| `kubelet_token_path` | string | "" | Kubelet Token文件路径 |
| `kubelet_ca_path` | string | "" | Kubelet CA证书路径 |
| `kubelet_skip_verify` | bool | false | 是否跳过证书验证 |

### 5. 智能限速配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `smart_limit_enabled` | bool | false | 是否启用智能限速 |
| `smart_limit_monitor_interval` | int | 60 | 监控间隔(秒) |
| `smart_limit_history_window` | int | 60 | 历史数据保留窗口(分钟) |
| `smart_limit_annotation_prefix` | string | "io-limit" | 注解前缀 |
| `smart_limit_windows` | []WindowConfig | 见下表 | 多窗口配置 |

#### WindowConfig 结构

| 字段 | 类型 | 说明 |
|------|------|------|
| `duation` | int | 窗口长度(分钟) |
| `iops_threshold` | int | IOPS阈值 |
| `bps_threshold` | int | BPS阈值(字节/秒) |

**默认窗口配置：**
- 1分钟窗口：IOPS=500, BPS=20MB/s
- 5分钟窗口：IOPS=800, BPS=30MB/s
- 30分钟窗口：IOPS=1000, BPS=50MB/s

### 6. 解除限速配置

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `smart_limit_remove_threshold` | float64 | 0.0 | 解除限速阈值(0.0-1.0) |
| `smart_limit_remove_delay` | int | 5 | 解除限速延迟(分钟) |
| `smart_limit_remove_check_interval` | int | 1 | 解除限速检查间隔(分钟) |

## 环境变量映射

所有配置项都可以通过环境变量覆盖，命名规则：

1. 将配置项名称转换为大写
2. 保持下划线不变

**示例：**
```bash
# 设置容器IOPS限制
export CONTAINER_IOPS_LIMIT=1000

# 启用智能限速
export SMART_LIMIT_ENABLED=true

# 设置Kubelet主机
export KUBELET_HOST=192.168.1.100

# 设置数据挂载点
export DATA_MOUNT=/mnt/data

# 设置排除关键词(逗号分隔)
export EXCLUDE_KEYWORDS="pause,istio-proxy,system"
```

## 使用示例

### 1. 使用默认配置

```bash
# 直接运行，使用内置默认值
./kubediskguard
```

### 2. 使用配置文件

```bash
# 复制默认配置并修改
cp config.default.yaml my-config.yaml
# 编辑 my-config.yaml

# 指定配置文件运行
export CONFIG_FILE_PATH=./my-config.yaml
./kubediskguard
```

### 3. 使用环境变量

```bash
# 通过环境变量覆盖特定配置
export CONTAINER_IOPS_LIMIT=1000
export SMART_LIMIT_ENABLED=true
export CONFIG_FILE_PATH=./my-config.yaml
./kubediskguard
```

### 4. 动态配置更新

```bash
# 启用动态配置监控
export CONFIG_FILE_PATH=./config.yaml
./kubediskguard

# 在另一个终端修改配置文件
echo "container_iops_limit: 800" >> config.yaml
# 程序会自动检测并重新加载配置
```

## 配置验证

程序启动时会验证配置的有效性：

- IOPS和BPS限制值必须 >= 0
- 端口号必须在有效范围内
- 文件路径必须存在且可访问
- 智能限速窗口配置必须合理

## 故障排除

### 1. 配置文件格式错误

```
ERROR: 配置文件解析失败: yaml: line 10: found character that cannot start any token
```

**解决方案：**
- 检查YAML/JSON语法
- 确保缩进正确
- 验证特殊字符转义

### 2. 环境变量类型错误

```
WARN: 环境变量 CONTAINER_IOPS_LIMIT 值无效: "abc"
```

**解决方案：**
- 确保数值类型环境变量设置为有效数字
- 布尔类型使用 "true"/"false"

### 3. 权限问题

```
ERROR: 无法访问容器运行时Socket: permission denied
```

**解决方案：**
- 确保程序有访问Docker/Containerd Socket的权限
- 考虑使用sudo或将用户加入docker组

## 最佳实践

1. **生产环境配置**
   - 使用配置文件而非环境变量管理复杂配置
   - 启用智能限速以获得更好的性能
   - 根据实际硬件调整IOPS和BPS限制

2. **开发环境配置**
   - 使用较宽松的限制值
   - 启用详细日志
   - 使用动态配置便于调试

3. **安全考虑**
   - 限制配置文件访问权限
   - 在生产环境中启用证书验证
   - 定期审查排除规则

4. **性能优化**
   - 根据工作负载调整监控间隔
   - 合理设置历史数据窗口
   - 监控系统资源使用情况