#!/bin/bash
# 兼容壳:运行生命周期已收敛到 make(start/stop/restart/status,见 CLAUDE.md §5)。
# 本脚本等价于 make restart,保留仅为兼容既有肌肉记忆与外部引用。
exec make restart
