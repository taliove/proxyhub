#!/bin/bash
# ProxyHub 快速启动脚本(运行态统一写入 var/,见 CLAUDE.md 目录范式)

LOG_DIR="./var/log"
LOG_FILE="$LOG_DIR/proxyhub.log"
mkdir -p "$LOG_DIR"

./dist/proxyhub > "$LOG_FILE" 2>&1 &
PID=$!
echo $PID > "$LOG_DIR/proxyhub.pid"
sleep 2

if ps -p $PID > /dev/null; then
    echo "✅ ProxyHub 已启动 (PID: $PID)"
    echo "   访问: http://localhost:8080"
    echo "   日志: $LOG_FILE"
else
    echo "❌ 启动失败，查看日志: cat $LOG_FILE"
    exit 1
fi
