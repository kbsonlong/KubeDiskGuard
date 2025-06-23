# 变更日志（Changelog）

## v2.2.0 2025-06-21

### 🚀 智能限速功能重大升级
- **智能限速模块**: 新增完整的智能限速功能，支持自动监控容器IO使用情况并动态调整限速
- **kubelet API集成**: 新增kubelet API客户端，支持通过kubelet API获取容器IO统计信息
- **cAdvisor计算器**: 新增cAdvisor指标计算模块，支持IOPS和BPS趋势分析
- **IO统计增强**: 在cgroup模块中新增IOStats结构体，支持获取容器的详细IO统计信息

### 📊 核心功能增强
- **智能限速配置**: 新增完整的智能限速配置项，支持通过环境变量配置监控参数
- **Pod管理优化**: 在kubeclient中新增GetPod和UpdatePod方法，支持Pod的获取和更新操作
- **服务集成**: 在service层集成智能限速管理器，优化Pod事件处理逻辑
- **注解解析增强**: 支持智能限速注解的解析和处理

### 🛠️ 开发工具与测试
- **测试工具**: 新增test-cadvisor-calculation和test-kubelet-api命令行工具
- **测试增强**: 在main_test.go中新增mockKubeClient的GetPod和UpdatePod方法
- **单元测试**: 新增smartlimit模块的完整单元测试覆盖
- **测试脚本**: 新增kubelet API测试脚本，支持高级测试场景

### 📚 文档完善
- **智能限速指南**: 新增SMART_LIMIT_GUIDE.md，详细说明智能限速功能的使用方法
- **kubelet API集成**: 新增KUBELET_API_INTEGRATION.md和KUBELET_API_SUMMARY.md
- **cAdvisor计算**: 新增CADVISOR_IO_CALCULATION.md，说明IO指标计算方法
- **架构对比**: 新增CGROUP_VS_CADVISOR_COMPARISON.md，对比不同数据源的优劣
- **容器IO计算**: 新增CONTAINER_FS_WRITES_TOTAL_CALCULATION.md，说明容器IO统计方法
- **README更新**: 大幅更新README.md，详细描述智能限速功能及配置示例

### 📋 示例与配置
- **智能限速示例**: 新增smart-limit-example.yaml，提供完整的智能限速配置示例
- **测试Pod示例**: 新增test-pod.yaml，用于验证智能限速功能
- **测试脚本**: 新增test-kubelet-api.sh和test-kubelet-api-advanced.sh脚本

### 🔧 技术架构优化
- **模块化设计**: 新增独立的smartlimit、kubelet、cadvisor模块
- **配置管理**: 扩展配置结构，支持智能限速相关参数
- **接口设计**: 优化kubeclient接口，支持Pod操作
- **数据流优化**: 支持kubelet API和cgroup双重数据源

### 📈 性能与可靠性
- **数据源多样化**: 支持kubelet API和cgroup文件系统两种IO数据获取方式
- **错误处理**: 增强错误处理和回退机制
- **监控精度**: 提高IO监控的精度和实时性
- **资源优化**: 优化内存使用和计算效率

---

## v2.1.0 2025-06-19

### 注解前缀统一优化
- **全局注解前缀变更**：将所有注解前缀从 `iops-limit` 统一变更为 `io-limit`，使注解命名更加简洁和一致
- **影响范围**：
  - `iops-limit/read-iops` → `io-limit/read-iops`
  - `iops-limit/write-iops` → `io-limit/write-iops`
  - `iops-limit/iops` → `io-limit/iops`
  - `iops-limit/read-bps` → `io-limit/read-bps`
  - `iops-limit/write-bps` → `io-limit/write-bps`
  - `iops-limit/bps` → `io-limit/bps`
  - `iops-limit` → `io-limit`
- **智能限速注解同步更新**：
  - `iops-limit/smart-limit` → `io-limit/smart-limit`
  - `iops-limit/auto-iops` → `io-limit/auto-iops`
  - `iops-limit/auto-bps` → `io-limit/auto-bps`
  - `iops-limit/limit-reason` → `io-limit/limit-reason`
- **配置项更新**：
  - 环境变量 `SMART_LIMIT_ANNOTATION_PREFIX` 默认值从 `iops-limit` 更新为 `io-limit`
- **测试用例修正**：更新所有相关测试用例中的注解前缀，确保测试通过
- **文档同步更新**：README.md、用户手册、开发手册等文档中的注解示例全部更新

### 注解解析逻辑优化
- **优先级调整**：明确 `io-limit/iops` 和 `io-limit/bps` 的优先级最高，分别覆盖读写IOPS和读写BPS设置
- **0值处理优化**：注解值为0时正确解除对应方向的限速，避免误判
- **兼容性保持**：保留对旧格式注解的兼容性，确保平滑升级

---

## v2.0.0 2025-06-19

### IOPS与BPS限速功能增强
- 注解支持：
  - IOPS：`io-limit/read-iops`、`io-limit/write-iops`、`io-limit/iops`（优先级：read-iops/write-iops > iops）
  - BPS：`io-limit/read-bps`、`io-limit/write-bps`、`io-limit/bps`
  - 注解为0时自动解除对应方向的限速
- 环境变量支持：
  - `CONTAINER_READ_IOPS_LIMIT`、`CONTAINER_WRITE_IOPS_LIMIT`、`CONTAINER_IOPS_LIMIT`
  - `CONTAINER_READ_BPS_LIMIT`、`CONTAINER_WRITE_BPS_LIMIT`
- cgroup v2下，IOPS和BPS限速通过同一个io.max文件一次性写入，避免互相覆盖。
- service层统一收集所有限速项，调用SetLimits/ResetLimits一次性下发或解除。

### 项目名称变更 `KubeDiskGuard`

- 功能范围扩展：项目已从单一的IOPS限速，扩展为同时支持IOPS和BPS（带宽）双向限速，具备更全面的磁盘IO资源管控能力。
- 定位更准确：新名称"KubeDiskGuard"突出"守护（Guard）"和"磁盘（Disk）"的核心价值，更好地体现了节点级磁盘IO资源的保护与管理。
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
- README.md已全面更新，所有io-limit/limit相关内容替换为io-limit/read-iops、io-limit/write-iops、io-limit/iops，并补充优先级、兼容性说明。
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
- **注解为0自动解除限速**：Pod注解`io-limit/limit: "0"`时，自动调用ResetIOPSLimit解除该Pod下所有容器的IOPS限速，无需手动命令。
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