# 启动阶段磁盘性能采集（Profiler）使用指南

## 功能概述
- 在节点进程启动时对数据盘挂载点执行轻量采样（随机 4K IOPS、顺序 128K BPS），生成设备能力基线。
- 采样结果持久化为本地 JSON 文件，后续启动在 TTL 内直接复用，避免重复压测。
- 在无显式 Pod 注解时，默认容器限速值将按基线的约 70% 进行设置，预留系统与其他负载空间。
- 全面兼容 cgroup v1 / v2 限速写入路径。

## 配置项（环境变量）
- PROFILER_ENABLED（默认 true）
- PROFILER_TTL_SECONDS（默认 86400）
- PROFILER_STORE_PATH（默认 /var/lib/kubediskguard/perf.json）
- PROFILER_MAX_LOADAVG（默认 1.5，超过则跳过采样）
- PROFILER_SAMPLE_DURATION（默认 8 秒）
- PROFILER_MAX_FILE_MB（默认 32MB）
- PROFILER_MOUNT_POINT（默认 /data）

## 运行与观测
- 指标暴露：kubediskguard_profiler_runs_total、kubediskguard_profiler_skips_total、kubediskguard_profiler_last_duration_seconds
- 健康探针：/healthz 返回 200
- 日志：启动阶段会输出“Load baseline / Sample baseline”与应用推荐值的日志

## 压测负载控制
- 采样任务串行执行，单设备总时长不超过配置的采样时长。
- 实时读取 /proc/loadavg，当负载超过阈值则跳过采样，避免影响业务。
- 采样使用小文件并自动清理，尽量减少磁盘占用与缓存干扰。

## 与容器运行时与 cgroup 的集成
- 设备号解析复用现有逻辑，限速在 Docker/containerd 下分别拼接 v1/v2 路径并写入对应文件。
- 详情见：[cgroup.go](file:///Users/zengshenglong/Code/GoWorkSpace/KubeDiskGuard/pkg/cgroup/cgroup.go)、[docker.go](file:///Users/zengshenglong/Code/GoWorkSpace/KubeDiskGuard/pkg/runtime/docker.go)、[containerd.go](file:///Users/zengshenglong/Code/GoWorkSpace/KubeDiskGuard/pkg/runtime/containerd.go)。

## 注意事项
- 若节点负载持续较高（如升级期或批量计算任务），采样会自动跳过并保留上次基线。
- 若无历史基线且负载过高，系统会继续使用默认限速值（可自行配置）。
