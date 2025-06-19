# Change Log

## v2024-06-19

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