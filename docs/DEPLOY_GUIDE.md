<p align="center">
  <img src="./logo.svg" width="120" alt="KubeDiskGuard Logo"/>
</p>

<h1 align="center">KubeDiskGuard</h1>
<p align="center">Kubernetes 节点级磁盘 IO 资源守护与限速服务</p> 

---

# 部署手册（Deploy Guide）

## 1. 镜像构建与推送
1. 构建二进制：
   ```bash
   go build -o KubeDiskGuard main.go
   ```
2. 构建镜像：
   ```bash
   docker build -t your-repo/KubeDiskGuard:latest .
   ```
3. 推送镜像到仓库：
   ```bash
   docker push your-repo/KubeDiskGuard:latest
   ```

## 2. DaemonSet部署YAML配置
- 建议以特权模式运行，挂载数据盘和cgroup目录
- 推荐通过Downward API注入`NODE_NAME`
- 主要环境变量见[用户手册](./USER_GUIDE.md)

**示例DaemonSet片段：**
```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: KubeDiskGuard
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: KubeDiskGuard
  template:
    metadata:
      labels:
        app: KubeDiskGuard
    spec:
      containers:
      - name: KubeDiskGuard
        image: your-repo/KubeDiskGuard:latest
        securityContext:
          privileged: true
          runAsUser: 0
          runAsGroup: 0
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: DATA_MOUNT
          value: "/data"
        # 其它环境变量...
        volumeMounts:
        - name: cgroup
          mountPath: /sys/fs/cgroup
        - name: data
          mountPath: /data
      volumes:
      - name: cgroup
        hostPath:
          path: /sys/fs/cgroup
      - name: data
        hostPath:
          path: /data
```

## 3. 常见部署问题与排查
- **容器未启动/CrashLoopBackOff**：检查特权模式、挂载点、环境变量
- **限速未生效**：确认注解/环境变量配置、cgroup路径、日志输出
- **Pod信息获取失败**：检查kubelet API权限、网络连通性
- **日志无输出**：确认日志级别、容器标准输出配置

## 4. 版本升级与回滚
- 升级：先推送新镜像，滚动更新DaemonSet
- 回滚：可通过`kubectl rollout undo ds/KubeDiskGuard -n kube-system`快速回滚
- 升级前建议先在测试环境验证

## 5. 生产环境最佳实践
- 只在业务节点部署，避免影响系统组件
- 合理配置过滤关键字和命名空间，避免误限速
- 定期关注[CHANGELOG.md](./CHANGELOG.md)和[用户手册](./USER_GUIDE.md)
- 建议配合监控系统（如Prometheus）跟踪IOPS/BPS指标

---
如有部署疑问，请联系运维支持团队。

