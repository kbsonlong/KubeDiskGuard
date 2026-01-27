# 使用 kind 进行限流感知验证（容器化工具链）

## 前置
- 确保 Docker 可用（Mac 可用 Docker Desktop）
- 本项目根目录下执行命令

## 创建/删除集群
```bash
scripts/kind-create.sh create
scripts/kubectl.sh get nodes
# 删除
scripts/kind-create.sh delete
```

## 构建加载并部署 KubeDiskGuard
```bash
scripts/kind-load-and-deploy.sh
scripts/kubectl.sh -n kube-system get pods -l app=kubediskguard
```

## 部署压测工作负载
```bash
scripts/kubectl.sh apply -f manifests/workload-fio.yaml
scripts/kubectl.sh -n io-test get pods
```

## 观测验证
- 事件：查看是否出现 CgroupIOLimited
```bash
scripts/kubectl.sh get events -A | grep CgroupIOLimited || true
```
- 注解：查看工作负载 Pod 注解（若被标记为 throttled）
```bash
scripts/kubectl.sh -n io-test get pod -o json \
  | jq '.items[0].metadata.annotations | with_entries(select(.key|test("io-limit/")))'
```
- 指标：端口转发或使用 NodePort 访问 /metrics，检查新增指标
  - kubediskguard_cgroup_throttled_ops_total
  - kubediskguard_cgroup_throttled_bytes_total
  - kubediskguard_cgroup_io_pressure_stall_seconds

## 可选：强制触发“节流”
- 在 kind 默认环境下未设置 io.max，节流计数可能保持为 0；可用 PSI（io.pressure）先验证 I/O stall。
- 若需强制节流验证，可在测试环境部署特权 DaemonSet 挂载 /sys/fs/cgroup，并针对目标容器 cgroup 路径写入低 io.max（仅用于本地验证，生产禁用）。

## 清理
```bash
scripts/kubectl.sh delete -f manifests/workload-fio.yaml
scripts/kind-create.sh delete
```
