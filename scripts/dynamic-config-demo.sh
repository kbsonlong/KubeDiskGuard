#!/bin/bash

# 动态配置加载功能演示脚本
# 此脚本演示如何使用 KubeDiskGuard 的动态配置功能

set -e

echo "=== KubeDiskGuard 动态配置功能演示 ==="
echo

# 1. 创建示例配置文件
echo "1. 创建示例配置文件..."
cat > /tmp/kubediskguard-config.yaml << 'EOF'
# KubeDiskGuard 动态配置示例
container_iops_limit: 1000
container_read_iops_limit: 800
container_write_iops_limit: 1200
data_mount: "/data"
smart_limit_enabled: true
smart_limit_monitor_interval: 60
kubelet_port: "10250"
container_runtime: "containerd"
EOF

echo "配置文件已创建: /tmp/kubediskguard-config.yaml"
echo "配置内容:"
cat /tmp/kubediskguard-config.yaml
echo

# 2. 设置环境变量
echo "2. 设置配置文件路径环境变量..."
export CONFIG_FILE_PATH="/tmp/kubediskguard-config.yaml"
echo "CONFIG_FILE_PATH=$CONFIG_FILE_PATH"
echo

# 3. 启动应用程序（后台运行）
echo "3. 启动 KubeDiskGuard（后台运行）..."
echo "注意：应用程序将在后台运行，您可以修改配置文件来测试动态加载功能"
echo

# 检查可执行文件是否存在
if [ ! -f "./kubediskguard" ]; then
    echo "错误：找不到 kubediskguard 可执行文件"
    echo "请先运行: go build -o kubediskguard main.go"
    exit 1
fi

# 启动应用程序
echo "启动应用程序..."
./kubediskguard &
APP_PID=$!
echo "应用程序已启动，PID: $APP_PID"
echo

# 等待应用程序初始化
echo "等待应用程序初始化..."
sleep 3
echo

# 4. 演示配置动态更新
echo "4. 演示配置动态更新..."
echo "修改配置文件中的 IOPS 限制..."

# 修改配置
cat > /tmp/kubediskguard-config.yaml << 'EOF'
# KubeDiskGuard 动态配置示例（已更新）
container_iops_limit: 2000
container_read_iops_limit: 1500
container_write_iops_limit: 2500
data_mount: "/updated-data"
smart_limit_enabled: false
smart_limit_monitor_interval: 120
kubelet_port: "10251"
container_runtime: "docker"
EOF

echo "配置已更新为:"
cat /tmp/kubediskguard-config.yaml
echo

echo "等待配置自动重新加载（最多等待 10 秒）..."
sleep 10
echo

# 5. 再次修改配置
echo "5. 再次修改配置以演示持续监听..."
cat > /tmp/kubediskguard-config.yaml << 'EOF'
# KubeDiskGuard 动态配置示例（第二次更新）
container_iops_limit: 3000
container_read_iops_limit: 2000
container_write_iops_limit: 4000
data_mount: "/final-data"
smart_limit_enabled: true
smart_limit_monitor_interval: 30
kubelet_port: "10252"
container_runtime: "containerd"
EOF

echo "配置再次更新为:"
cat /tmp/kubediskguard-config.yaml
echo

echo "等待配置自动重新加载（最多等待 10 秒）..."
sleep 10
echo

# 6. 清理
echo "6. 清理演示环境..."
echo "停止应用程序..."
kill $APP_PID 2>/dev/null || true
wait $APP_PID 2>/dev/null || true

echo "删除临时配置文件..."
rm -f /tmp/kubediskguard-config.yaml

echo
echo "=== 演示完成 ==="
echo
echo "总结:"
echo "1. 应用程序成功启动并加载了初始配置"
echo "2. 配置文件修改后，应用程序自动检测并重新加载了配置"
echo "3. 整个过程无需重启应用程序"
echo "4. 您可以在日志中看到配置更新的详细信息"
echo
echo "使用提示:"
echo "- 设置环境变量 CONFIG_FILE_PATH 指定配置文件路径"
echo "- 支持 JSON 和 YAML 格式的配置文件"
echo "- 配置文件变化会在 5 秒内被检测到"
echo "- 环境变量配置优先级高于文件配置"
echo