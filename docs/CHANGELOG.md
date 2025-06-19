# 变更日志（Changelog）

## v2.0.0 2025-06-19

### IOPS与BPS限速功能增强
- 注解支持：
  - IOPS：`iops-limit/read-iops`、`iops-limit/write-iops`、`iops-limit/iops`（优先级：read-iops/write-iops > iops）
  - BPS：`iops-limit/read-bps`、`iops-limit/write-bps`、`iops-limit/bps`
  - 注解为0时自动解除对应方向的限速
- 环境变量支持：
  - `CONTAINER_READ_IOPS_LIMIT`、`CONTAINER_WRITE_IOPS_LIMIT`、`CONTAINER_IOPS_LIMIT`
  - `CONTAINER_READ_BPS_LIMIT`、`CONTAINER_WRITE_BPS_LIMIT`
- cgroup v2下，IOPS和BPS限速通过同一个io.max文件一次性写入，避免互相覆盖。
- service层统一收集所有限速项，调用SetLimits/ResetLimits一次性下发或解除。

### 项目名称变更 `KubeDiskGuard`

- 功能范围扩展：项目已从单一的IOPS限速，扩展为同时支持IOPS和BPS（带宽）双向限速，具备更全面的磁盘IO资源管控能力。
- 定位更准确：新名称“KubeDiskGuard”突出“守护（Guard）”和“磁盘（Disk）”的核心价值，更好地体现了节点级磁盘IO资源的保护与管理。
- 便于后续扩展：新名称不局限于IOPS，未来可扩展更多磁盘相关功能（如监控、告警、自动调优等），提升项目的可维护性和影响力。
- 中英文统一，易于传播：KubeDiskGuard 简洁易记，配合中文副标题，便于在国内外社区推广和应用。

### 代码重构与接口精简
- Runtime接口精简，只保留SetLimits/ResetLimits，去除SetIOPSLimit/SetBPSLimit等冗余方法。
- cgroup.Manager实现SetLimits/ResetLimits，v1分文件写，v2一次性写io.max。
- service层processPodContainers支持读写IOPS、BPS分开限速，且合并为一次性调用。
- 注解变更判断时，readIops和writeIops都要判断，podAnnotations结构体同步调整。
- 删除了GetContainers等全量遍历相关接口及实现。

### 冗余代码与测试清理
- 删除了docker/containerd runtime中所有未被调用的SetIOPSLimit、ResetIOPSLimit、SetBPSLimit、ResetBPSLimit等冗余方法。
- 删除了GetContainers等批量接口及其mock实现。
- 清理了main_test.go中所有依赖废弃接口的mock和测试用例。
- mock runtime与mock kubeclient只保留主流程相关接口，测试代码更聚焦。

### 健壮性与单元测试
- 单元测试覆盖ParseIopsLimitFromAnnotations、ParseBpsLimitFromAnnotations、service层processPodContainers等所有核心分支，验证注解优先级、0值解除、默认值等场景。
- ShouldProcessPod支持Started字段判断，只有所有业务容器Started为true才处理，避免容器未就绪时误操作。

### 文档与兼容性
- README.md已全面更新，所有iops-limit/limit相关内容替换为iops-limit/read-iops、iops-limit/write-iops、iops-limit/iops，并补充优先级、兼容性说明。
- 环境变量表格、注解示例、优先级说明、主要变更等均已同步。
- 变更历史已写入CHANGELOG.md，详细记录架构优化、功能增强、接口调整、测试完善等内容。

---

## v1.0.0 2025-06-19

### 重大架构优化
- **以Pod为主索引**：所有限速和过滤逻辑均以Pod及其containerStatuses为入口，彻底避免全量遍历容器运行时，极大提升性能和准确性。
- **运行时只做单容器操作**：只在需要底层操作（如cgroup限速）时，用runtime ID查单个容器详细信息，避免全量遍历。
- **事件监听、注解变更、服务重启等场景全部用Pod+containerStatuses实现**，保证与K8s调度状态强一致。
- **代码结构清晰**：service层负责业务主流程和过滤，runtime层只负责单容器操作。

### 功能增强与行为变更
- **kubelet API优先**：获取本节点Pod信息时优先通过kubelet API，只有kubelet API不可用时才fallback到API Server，极大减少apiserver压力。
- **注解为0自动解除限速**：Pod注解`iops-limit/limit: "0"`时，自动调用ResetIOPSLimit解除该Pod下所有容器的IOPS限速，无需手动命令。
- **Started字段判断更严谨**：只有当Pod为Running且所有业务容器的`Started`字段为true（即startupProbe通过、postStart钩子执行完毕）时才进行IOPS限速，避免容器未就绪时误操作。
- **main.go参数精简**：移除`resetOne`参数，所有解除操作均通过注解声明式完成。

### 配置与部署
- **支持KUBECONFIG_PATH**：支持集群外部运行，便于本地开发和调试。
- **DaemonSet推荐通过Downward API注入NODE_NAME**，并完善了相关环境变量文档。

### 单元测试与健壮性
- **单元测试完善**：覆盖注解为0自动解除、Started字段多种情况、ShouldProcessPod等核心分支。
- **mock runtime与mock kubeclient**：便于隔离测试业务逻辑。

### 其它
- **README.md与注释同步更新**，并新增本CHANGELOG文档。 