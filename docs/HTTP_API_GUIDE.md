# KubeDiskGuard HTTP API 指南

## 概述

KubeDiskGuard 现在提供了 HTTP API 接口，用于查询计算后的 metrics 数据。API 服务器集成在主程序中，与 Prometheus metrics 和健康检查共享同一个端口（默认 2112）。

## API 基础信息

- **基础 URL**: `http://localhost:2112/api/v1`
- **响应格式**: JSON
- **支持的 HTTP 方法**: GET

## API 接口列表

### 1. 容器指标接口

#### 获取所有容器指标
```
GET /api/v1/containers
```

**查询参数**:
- `limit` (int): 返回结果数量限制，默认 100
- `include_trend` (bool): 是否包含趋势数据，默认 false
- `include_history` (bool): 是否包含历史数据，默认 false
- `namespace` (string): 按命名空间过滤
- `pod` (string): 按 Pod 名称过滤（支持部分匹配）

**示例**:
```bash
curl "http://localhost:2112/api/v1/containers?include_trend=true&limit=50"
```

#### 获取单个容器指标
```
GET /api/v1/containers/{containerID}
```

**路径参数**:
- `containerID` (string): 容器 ID

**查询参数**:
- `include_trend` (bool): 是否包含趋势数据
- `include_history` (bool): 是否包含历史数据

**示例**:
```bash
curl "http://localhost:2112/api/v1/containers/abc123?include_trend=true&include_history=true"
```

### 2. 限速状态接口

#### 获取所有容器限速状态
```
GET /api/v1/limit-status
```

**查询参数**:
- `limit` (int): 返回结果数量限制，默认 100
- `namespace` (string): 按命名空间过滤
- `pod` (string): 按 Pod 名称过滤
- `only_limited` (bool): 只返回被限速的容器，默认 false

**示例**:
```bash
curl "http://localhost:2112/api/v1/limit-status?only_limited=true"
```

#### 获取单个容器限速状态
```
GET /api/v1/limit-status/{containerID}
```

**路径参数**:
- `containerID` (string): 容器 ID

**示例**:
```bash
curl "http://localhost:2112/api/v1/limit-status/abc123"
```

### 3. 系统信息接口

#### 健康检查
```
GET /api/v1/health
```

**示例**:
```bash
curl "http://localhost:2112/api/v1/health"
```

#### 系统信息
```
GET /api/v1/info
```

**示例**:
```bash
curl "http://localhost:2112/api/v1/info"
```

## 响应格式

### 标准响应结构
```json
{
  "success": true,
  "message": "操作成功",
  "data": {},
  "count": 10
}
```

### 容器指标响应
```json
{
  "success": true,
  "data": [
    {
      "container_id": "abc123",
      "pod_name": "test-pod",
      "namespace": "default",
      "last_update": "2024-01-01T12:00:00Z",
      "trend": {
        "read_iops_15m": 100.5,
        "write_iops_15m": 50.2,
        "read_bps_15m": 1048576,
        "write_bps_15m": 524288,
        "read_iops_30m": 95.3,
        "write_iops_30m": 48.1,
        "read_bps_30m": 1000000,
        "write_bps_30m": 500000,
        "read_iops_60m": 90.1,
        "write_iops_60m": 45.5,
        "read_bps_60m": 950000,
        "write_bps_60m": 475000
      },
      "history": [
        {
          "timestamp": "2024-01-01T11:59:00Z",
          "read_iops": 102.3,
          "write_iops": 51.1,
          "read_bps": 1050000,
          "write_bps": 525000
        }
      ]
    }
  ],
  "count": 1
}
```

### 限速状态响应
```json
{
  "success": true,
  "data": [
    {
      "container_id": "abc123",
      "pod_name": "test-pod",
      "namespace": "default",
      "is_limited": true,
      "triggered_by": "15m",
      "limit_result": {
        "triggered_by": "15m",
        "read_iops": 1000,
        "write_iops": 1000,
        "read_bps": 10485760,
        "write_bps": 10485760,
        "reason": "15m窗口触发: 读IOPS=102.3 > 100, 写IOPS=51.1 > 50"
      },
      "applied_at": "2024-01-01T12:00:00Z",
      "last_check_at": "2024-01-01T12:01:00Z"
    }
  ],
  "count": 1
}
```

## 错误响应

```json
{
  "success": false,
  "message": "Container not found"
}
```

## 测试脚本

项目根目录提供了测试脚本 `test_api.sh`，可以快速测试所有 API 接口：

```bash
./test_api.sh
```

## 集成说明

### 现有服务集成

API 服务器已集成到现有的 HTTP 服务器中，与以下接口共享端口：
- `/metrics` - Prometheus 指标
- `/healthz` - 原有健康检查
- `/api/v1/*` - 新增 API 接口

### 数据来源

API 接口直接从 `SmartLimitManager` 获取数据：
- **容器历史数据**: 通过 `GetAllContainerHistory()` 和 `GetContainerHistory()` 获取
- **IO 趋势数据**: 通过 `AnalyzeAllContainerTrends()` 计算
- **限速状态**: 通过 `GetAllLimitStatus()` 和 `GetContainerLimitStatus()` 获取

### 中间件

API 服务器包含以下中间件：
- **CORS 中间件**: 支持跨域请求
- **日志中间件**: 记录请求日志

## 使用场景

1. **监控面板**: 构建实时监控界面显示容器 IO 指标
2. **告警系统**: 基于限速状态触发告警
3. **性能分析**: 分析容器 IO 趋势和历史数据
4. **自动化运维**: 通过 API 集成到运维工具中

## 注意事项

1. API 服务器与主程序共享同一进程，无需额外部署
2. 数据实时性取决于 SmartLimitManager 的监控周期
3. 历史数据的保留时间由配置决定
4. 大量容器环境下建议使用分页和过滤参数