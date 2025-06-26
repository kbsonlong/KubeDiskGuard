#!/bin/bash

# KubeDiskGuard 热重载功能演示脚本
# 展示如何在不重启服务的情况下动态更新配置

set -e

echo "=== KubeDiskGuard 热重载功能演示 ==="

# 检查必要的环境变量
if [ -z "$NODE_NAME" ]; then
    echo "错误: 请设置 NODE_NAME 环境变量"
    echo "示例: export NODE_NAME=\"your-node-name\""
    exit 1
fi

# 创建演示配置文件目录
DEMO_CONFIG_DIR=$(mktemp -d)
echo "演示配置文件目录: $DEMO_CONFIG_DIR"

# 清理函数
cleanup() {
    echo ""
    echo "清理演示文件..."
    rm -rf "$DEMO_CONFIG_DIR"
    if [ ! -z "$PID" ]; then
        echo "停止演示服务 PID: $PID"
        kill $PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# 创建初始配置文件
echo "创建初始配置文件..."
cat > "$DEMO_CONFIG_DIR/config.json" << 'EOF'
{
    "container_iops_limit": 1000,
    "container_read_iops_limit": 500,
    "container_write_iops_limit": 500,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 30,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"]
}
EOF

echo "✓ 初始配置文件已创建"

# 启动服务（后台运行）
echo ""
echo "启动 KubeDiskGuard 服务..."
export CONFIG_FILE_PATH="$DEMO_CONFIG_DIR/config.json"
export NODE_NAME="$NODE_NAME"

# 启动服务并捕获PID
./iops-limit-service -metrics-addr :2112 > "$DEMO_CONFIG_DIR/service.log" 2>&1 &
PID=$!

echo "✓ 服务已启动，PID: $PID"

# 等待服务启动
echo "等待服务启动..."
sleep 5

# 检查服务是否正常启动
if ! curl -s http://localhost:2112/healthz > /dev/null; then
    echo "✗ 服务启动失败，请检查日志:"
    cat "$DEMO_CONFIG_DIR/service.log"
    exit 1
fi

echo "✓ 服务启动成功"

# 演示1: 更新IOPS限制
echo ""
echo "=== 演示1: 更新IOPS限制 ==="
echo "当前配置: IOPS限制 = 1000"
echo "更新为: IOPS限制 = 2000"

cat > "$DEMO_CONFIG_DIR/config.json" << 'EOF'
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 30,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"]
}
EOF

echo "✓ 配置文件已更新"
echo "等待热重载..."
sleep 8

# 检查热重载日志
if grep -q "热重载成功完成" "$DEMO_CONFIG_DIR/service.log"; then
    echo "✓ 热重载成功！IOPS限制已更新为2000"
else
    echo "✗ 热重载未成功"
fi

# 演示2: 更新监控间隔
echo ""
echo "=== 演示2: 更新监控间隔 ==="
echo "当前配置: 监控间隔 = 30秒"
echo "更新为: 监控间隔 = 60秒"

cat > "$DEMO_CONFIG_DIR/config.json" << 'EOF'
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 60,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"]
}
EOF

echo "✓ 配置文件已更新"
echo "等待热重载..."
sleep 8

# 检查监控循环重启日志
if grep -q "监控循环已重启" "$DEMO_CONFIG_DIR/service.log"; then
    echo "✓ 监控循环重启成功！监控间隔已更新为60秒"
else
    echo "✗ 监控循环重启未成功"
fi

# 演示3: 禁用智能限速
echo ""
echo "=== 演示3: 禁用智能限速 ==="
echo "当前配置: 智能限速 = 启用"
echo "更新为: 智能限速 = 禁用"

cat > "$DEMO_CONFIG_DIR/config.json" << 'EOF'
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "smart_limit_enabled": false,
    "smart_limit_monitor_interval": 60,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"]
}
EOF

echo "✓ 配置文件已更新"
echo "等待热重载..."
sleep 8

# 检查智能限速停止日志
if grep -q "智能限速管理器已停止" "$DEMO_CONFIG_DIR/service.log"; then
    echo "✓ 智能限速禁用成功！"
else
    echo "✗ 智能限速禁用未成功"
fi

# 演示4: 重新启用智能限速
echo ""
echo "=== 演示4: 重新启用智能限速 ==="
echo "当前配置: 智能限速 = 禁用"
echo "更新为: 智能限速 = 启用"

cat > "$DEMO_CONFIG_DIR/config.json" << 'EOF'
{
    "container_iops_limit": 2000,
    "container_read_iops_limit": 1000,
    "container_write_iops_limit": 1000,
    "smart_limit_enabled": true,
    "smart_limit_monitor_interval": 60,
    "smart_limit_annotation_prefix": "kubediskguard.io",
    "exclude_keywords": ["pause", "kube-proxy"],
    "exclude_namespaces": ["kube-system"]
}
EOF

echo "✓ 配置文件已更新"
echo "等待热重载..."
sleep 8

# 检查智能限速启动日志
if grep -q "智能限速管理器已启动" "$DEMO_CONFIG_DIR/service.log"; then
    echo "✓ 智能限速重新启用成功！"
else
    echo "✗ 智能限速重新启用未成功"
fi

echo ""
echo "=== 热重载演示完成 ==="
echo ""
echo "服务日志摘要:"
echo "=================="
tail -15 "$DEMO_CONFIG_DIR/service.log"

echo ""
echo "演示要点:"
echo "1. 配置文件变化会自动触发热重载"
echo "2. 服务无需重启即可应用新配置"
echo "3. 智能限速组件会根据配置自动启动/停止"
echo "4. 监控间隔变化会自动重启监控循环"
echo "5. 所有现有容器会重新处理以应用新配置"
echo ""
echo "详细说明请参考: docs/HOT_RELOAD_GUIDE.md" 