# ProxyHub 0.1.0 验收测试清单

本清单用于 0.1.0 正式发布前的人工验收测试。每项测试均有明确的通过/失败标准。**不自动化** — 这是发布前的人工门禁。

测试环境：
- **OS**: Ubuntu 22.04 LTS (amd64)
- **架构**： amd64
- **域名**： 测试用域名（DNS 已配置）
- **网络**： 公网可访问

---

## 测试 1: 交互式安装（全新服务器）

### 前置条件
- [ ] 全新 Ubuntu 22.04 服务器（未安装 ProxyHub）
- [ ] DNS A 记录已指向服务器 IP
- [ ] Caddy 未安装（测试首次安装）

### 步骤
1. SSH 登录服务器
2. 运行安装器：
   ```bash
   bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh)
   ```
3. 按提示输入域名（如 `test1.example.com`）
4. 按提示输入邮箱（或留空）
5. 等待安装完成

### 通过标准
- [ ] 安装器输出包含管理员凭证（账户名 + 密码 + Site Path）
- [ ] 安装器输出"安装成功"消息
- [ ] 访问 `https://<domain>/<site-path>/health` 返回 200 OK
- [ ] 使用凭证登录管理后台成功
- [ ] systemd 服务运行中：
  ```bash
  systemctl status proxyhub | grep "active (running)"
  ```
- [ ] Caddy 服务运行中：
  ```bash
  systemctl status caddy | grep "active (running)"
  ```
- [ ] 文件存在且权限正确：
  ```bash
  ls -l /usr/local/bin/proxyhub          # -rwxr-xr-x root:root
  ls -l /usr/local/bin/proxyhubctl       # -rwxr-xr-x root:root
  ls -l /etc/proxyhub/config.yaml        # -rw-r--r-- root:root
  ls -ld /var/lib/proxyhub               # drwxr-xr-x proxyhub:proxyhub
  ls -l /root/.proxyhub-install-info     # -rw------- root:root
  ```

### 失败标准
- 任何步骤报错退出
- 健康检查端点返回非 200
- 凭证无法登录
- 服务未运行

---

## 测试 2: 非交互式安装（CI/CD 场景）

### 前置条件
- [ ] 全新 Ubuntu 22.04 服务器（与测试1不同）
- [ ] DNS A 记录已指向服务器 IP
- [ ] Caddy 未安装

### 步骤
1. 运行非交互式安装：
   ```bash
   bash install.sh \
     --non-interactive \
     --domain test2.example.com \
     --email ci@example.com \
     --version latest
   ```
2. 等待安装完成（无提示交互）

### 通过标准
- [ ] 安装完成无需任何输入
- [ ] 凭证输出到 stdout
- [ ] 健康检查端点可访问
- [ ] 服务正常运行
- [ ] `/root/.proxyhub-install-info` 包含安装记录

### 失败标准
- 需要交互输入
- 安装失败退出

---

## 测试 3: 首次登录与基本操作

### 前置条件
- [ ] 测试1 或测试2 已通过
- [ ] 浏览器可访问管理后台

### 步骤
1. 访问 `https://<domain>/<site-path>/`
2. 使用凭证登录
3. 进入"机场管理" → 添加测试机场订阅
4. 等待首次聚合完成（约 1 分钟）
5. 进入"节点状态" → 查看节点列表
6. 进入"订阅地址" → 创建订阅地址
7. 复制订阅 URL（包含 token）
8. 在浏览器访问订阅 URL（应返回 Clash YAML）

### 通过标准
- [ ] 登录成功，显示仪表盘
- [ ] 机场添加成功，状态为"正常"
- [ ] 节点列表非空（至少 1 个节点）
- [ ] 订阅地址创建成功
- [ ] 访问订阅 URL 返回 Clash YAML（包含节点）
- [ ] 订阅拉取统计增加 1 次

### 失败标准
- 登录失败
- 机场添加后无节点
- 订阅 URL 返回错误

---

## 测试 4: 手动备份

### 前置条件
- [ ] 测试3 已通过（系统有数据）

### 步骤
1. 运行备份：
   ```bash
   proxyhubctl backup
   ```
2. 检查备份文件：
   ```bash
   ls -lh /var/lib/proxyhub/backups/
   ```
3. 验证备份可读取（不解密，仅检查文件头）：
   ```bash
   file /var/lib/proxyhub/backups/proxyhub-backup-*.tar.gz.enc
   ```

### 通过标准
- [ ] 备份命令成功（exit code 0）
- [ ] 输出包含备份文件路径
- [ ] 备份文件存在且大小 > 0
- [ ] 文件类型为 "openssl enc" 或 "data"（加密格式）
- [ ] `/var/lib/proxyhub/.last-backup` 文件更新

### 失败标准
- 备份命令报错
- 备份文件不存在或大小为 0

---

## 测试 5: 备份恢复

### 前置条件
- [ ] 测试4 已通过（有备份文件）

### 步骤
1. 记录当前数据库哈希：
   ```bash
   md5sum /var/lib/proxyhub/data.db
   ```
2. 添加一条新机场（用于验证恢复后数据回退）
3. 恢复备份：
   ```bash
   proxyhubctl restore /var/lib/proxyhub/backups/proxyhub-backup-*.tar.gz.enc --yes
   ```
4. 检查数据库哈希（应与步骤1 相同）
5. 登录管理后台，检查步骤2 添加的机场已消失

### 通过标准
- [ ] 恢复命令成功
- [ ] 数据库哈希与备份前一致
- [ ] 状态指纹验证通过（输出包含 "fingerprint verified"）
- [ ] 服务自动重启
- [ ] 管理后台可访问
- [ ] 新添加的数据已回退

### 失败标准
- 恢复失败
- 数据库损坏
- 服务无法启动

---

## 测试 6: 在线更新（happy path）

### 前置条件
- [ ] 测试5 已通过
- [ ] 当前版本不是最新版本（或使用 `--version` 指定旧版本重新安装）

### 步骤
1. 记录当前版本：
   ```bash
   proxyhub --version
   ```
2. 运行更新：
   ```bash
   proxyhubctl update
   ```
3. 检查新版本：
   ```bash
   proxyhub --version
   ```
4. 验证服务正常：
   ```bash
   proxyhubctl status
   curl https://<domain>/<site-path>/health
   ```

### 通过标准
- [ ] 更新前自动创建备份
- [ ] 下载新版本并验证 SHA256
- [ ] 版本号已更新
- [ ] 服务自动重启
- [ ] 健康检查端点返回 200
- [ ] 状态指纹验证通过（数据未丢失）
- [ ] 管理后台数据完整（机场、订阅地址、节点）

### 失败标准
- 更新失败
- 版本号未更新
- 数据丢失

---

## 测试 7: 更新回滚（模拟失败）

### 前置条件
- [ ] 测试6 已通过

### 步骤
1. 手动破坏二进制文件（模拟损坏下载）：
   ```bash
   # 备份真实二进制
   cp /usr/local/bin/proxyhub /tmp/proxyhub.bak
   # 替换为损坏文件
   echo "broken" > /tmp/proxyhub-fake
   chmod +x /tmp/proxyhub-fake
   ```
2. 模拟更新失败（手动执行 proxyhubctl update 的部分步骤）：
   ```bash
   systemctl stop proxyhub
   cp /tmp/proxyhub-fake /usr/local/bin/proxyhub
   # 尝试启动（应失败）
   systemctl start proxyhub || echo "Expected failure"
   ```
3. 手动回滚：
   ```bash
   cp /tmp/proxyhub.bak /usr/local/bin/proxyhub
   systemctl start proxyhub
   ```
4. 验证服务恢复：
   ```bash
   proxyhubctl status
   ```

### 通过标准
- [ ] 损坏的二进制无法启动服务
- [ ] 回滚到旧版本后服务正常
- [ ] 数据未丢失（管理后台可访问）

### 失败标准
- 回滚后服务无法启动
- 数据损坏

**注**： 真实的 `proxyhubctl update` 应自动检测失败并回滚。此测试验证手动回滚路径可行。

---

## 测试 8: Site Path 轮换

### 前置条件
- [ ] 测试7 已通过

### 步骤
1. 记录当前 Site Path:
   ```bash
   grep site_path /root/.proxyhub-install-info
   ```
2. 轮换 Site Path:
   ```bash
   proxyhubctl rotate-path
   ```
3. 记录新 Site Path（从输出中）
4. 验证旧 Site Path 已失效：
   ```bash
   curl https://<domain>/<old-site-path>/health  # 应返回 404
   ```
5. 验证新 Site Path 可用：
   ```bash
   curl https://<domain>/<new-site-path>/health  # 应返回 200
   ```
6. 使用新 URL 登录管理后台

### 通过标准
- [ ] 轮换命令成功
- [ ] 输出包含新 Site Path
- [ ] `/root/.proxyhub-install-info` 已更新
- [ ] Caddy 配置已更新（检查 `/etc/caddy/conf.d/proxyhub.caddy`）
- [ ] 旧 Site Path 返回 404
- [ ] 新 Site Path 返回 200
- [ ] 管理后台可用新 URL 访问

### 失败标准
- 轮换失败
- 旧 Site Path 仍可用（安全风险）
- 新 Site Path 不可用

---

## 测试 9: 自动更新启用/禁用

### 前置条件
- [ ] 测试8 已通过

### 步骤
1. 启用自动更新：
   ```bash
   proxyhubctl auto-update enable
   ```
2. 检查 cron 任务：
   ```bash
   crontab -l | grep proxyhubctl
   ```
3. 禁用自动更新：
   ```bash
   proxyhubctl auto-update disable
   ```
4. 验证 cron 任务已移除：
   ```bash
   crontab -l | grep proxyhubctl || echo "Cron removed (expected)"
   ```

### 通过标准
- [ ] 启用命令成功
- [ ] crontab 包含 `proxyhubctl update` 任务
- [ ] 禁用命令成功
- [ ] crontab 不再包含任务

### 失败标准
- 启用/禁用失败
- cron 任务未正确添加/移除

---

## 测试 10: 卸载（保留备份）

### 前置条件
- [ ] 测试9 已通过
- [ ] 已创建最终备份：
  ```bash
  proxyhubctl backup
  ```

### 步骤
1. 运行卸载：
   ```bash
   proxyhubctl uninstall
   ```
2. 选择"保留备份"（yes）
3. 验证文件已移除：
   ```bash
   ls /usr/local/bin/proxyhub 2>&1 | grep "No such file"
   ls /etc/proxyhub 2>&1 | grep "No such file"
   ls /var/lib/proxyhub 2>&1 | grep "No such file"
   ```
4. 验证备份已保存：
   ```bash
   ls -lh /root/proxyhub-uninstall-backup-*.tar.gz.enc
   ```
5. 验证 Caddy 配置已移除：
   ```bash
   ls /etc/caddy/conf.d/proxyhub.caddy 2>&1 | grep "No such file"
   ```
6. 验证 systemd 单元已移除：
   ```bash
   systemctl status proxyhub 2>&1 | grep "could not be found"
   ```

### 通过标准
- [ ] 卸载命令成功
- [ ] 所有 ProxyHub 文件已移除
- [ ] 备份归档存在于 `/root/`
- [ ] Caddy 配置已清理
- [ ] systemd 单元已移除
- [ ] `proxyhub` 用户已删除：
  ```bash
  id proxyhub 2>&1 | grep "no such user"
  ```

### 失败标准
- 卸载失败
- 文件残留
- 备份丢失

---

## 测试 11: 重新安装（覆盖检测）

### 前置条件
- [ ] 测试10 已通过（系统已卸载）

### 步骤
1. 重新运行安装器（交互式）：
   ```bash
   bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh)
   ```
2. 按提示完成安装
3. 验证服务正常
4. 尝试再次运行安装器（应拒绝）：
   ```bash
   bash install.sh --non-interactive --domain test.example.com
   ```

### 通过标准
- [ ] 重新安装成功（全新安装）
- [ ] 服务正常运行
- [ ] 第二次运行安装器被拒绝（检测到已安装）
- [ ] 错误消息提示使用 `proxyhubctl update` 升级或 `proxyhubctl uninstall` 卸载

### 失败标准
- 重新安装失败
- 安装器允许重复安装（数据冲突风险）

---

## 测试 12: Docker Caddy 模式安装（host 网络容器）

### 前置条件
- [ ] 全新 Ubuntu 22.04 服务器（未安装 ProxyHub、宿主机无 caddy 二进制）
- [ ] DNS A 记录已指向服务器 IP
- [ ] Docker ≥ 20.10 已安装，root 可直接执行 docker 命令
- [ ] host 网络 caddy 容器运行中：
  ```bash
  mkdir -p /srv/caddy
  docker run -d --name caddy-host --network host \
    -v /srv/caddy:/etc/caddy \
    caddy:2
  ```

### 步骤
1. 运行非交互式安装，显式指名容器：
   ```bash
   bash install.sh \
     --non-interactive \
     --domain test12.example.com \
     --caddy-docker caddy-host
   ```
2. 等待安装完成

### 通过标准
- [ ] 安装日志包含 docker caddy 模式与所选容器名
- [ ] 安装日志提示 host 网络、回环拓扑不变
- [ ] 配置片段写入 `/srv/caddy/conf.d/proxyhub.caddy`，宿主侧 Caddyfile 追加 `import /etc/caddy/conf.d/*.caddy`
- [ ] `/etc/proxyhub/config.yaml` 的 `server.host` 仍为 `127.0.0.1`（回环不变）
- [ ] `/root/.proxyhub-install-info` 含 `CADDY_MODE=docker` 与 `CADDY_CONTAINER=caddy-host`
- [ ] 访问 `https://<domain>/<site-path>/health` 返回 200 OK
- [ ] `proxyhubctl rotate-path` 成功（docker 通道重写片段并重载容器），新 Site Path 可访问

### 失败标准
- 安装器误走 native 路径或误报 Caddy 缺失
- 片段写进容器层而非挂载目录
- rotate-path 报错或未走 docker 通道

---

## 测试 13: Docker Caddy 模式安装（桥接网络容器）

### 前置条件
- [ ] 全新 Ubuntu 22.04 服务器（未安装 ProxyHub、宿主机无 caddy 二进制）
- [ ] DNS A 记录已指向服务器 IP
- [ ] Docker ≥ 20.10 已安装，root 可直接执行 docker 命令
- [ ] 桥接网络 caddy 容器运行中（发布 80/443、带 host-gateway 映射）：
  ```bash
  mkdir -p /srv/caddy-bridge
  docker run -d --name caddy-bridge \
    -p 80:80 -p 443:443 \
    -v /srv/caddy-bridge:/etc/caddy \
    --add-host host.docker.internal:host-gateway \
    caddy:2
  ```

### 步骤
1. 运行非交互式安装（不指名容器，验证单容器自动探测）：
   ```bash
   bash install.sh \
     --non-interactive \
     --domain test13.example.com
   ```
2. 等待安装完成，阅读安装摘要

### 通过标准
- [ ] 安装日志明示自动选用了唯一候选容器 `caddy-bridge`
- [ ] 安装日志打印网桥网关 IP 与 trusted 子网
- [ ] 安装摘要包含网桥信任边界警告（信任边界扩到该 docker 网桥、同网桥容器可伪造 XFF）
- [ ] `/etc/proxyhub/config.yaml` 的 `server.host` 为网桥网关 IP,`trusted_proxies` 为网桥子网
- [ ] 配置片段的反代目标为 `host.docker.internal:8080`
- [ ] 访问 `https://<domain>/<site-path>/health` 返回 200 OK
- [ ] 负向抽查：用缺 `--add-host host.docker.internal:host-gateway` 的桥接容器重新安装，应 fail closed 并打印修复指引

### 失败标准
- 安装器绑定 127.0.0.1 导致容器反代不通
- 安装摘要缺少信任边界警告
- 缺 host-gateway 映射时安装未被拒绝

---

## 验收决策

### 通过标准（全部满足）
- [ ] 测试 1-13 全部通过
- [ ] 无 CRITICAL 或 HIGH 优先级 bug
- [ ] 文档完整（DEPLOY.md + SECURITY.md）
- [ ] 安装器和 proxyhubctl 帮助文档准确
- [ ] 日志无明显错误或警告

### 可接受的已知问题（不阻塞发布）
- MEDIUM 优先级 bug（在 CHANGELOG 中记录）
- 性能优化机会（在 TODO 中记录）
- 文档小错误（拼写、格式）

### 不可接受（必须修复）
- 任何安全漏洞
- 数据丢失风险
- 升级失败无法回滚
- 备份无法恢复
- 服务无法启动

---

## 测试结果记录

测试执行人： _______________  
测试日期： _______________  
ProxyHub 版本： _______________  

| 测试编号 | 测试名称 | 结果 | 备注 |
|---------|---------|------|------|
| 1 | 交互式安装 | ☐ PASS ☐ FAIL | |
| 2 | 非交互式安装 | ☐ PASS ☐ FAIL | |
| 3 | 首次登录与基本操作 | ☐ PASS ☐ FAIL | |
| 4 | 手动备份 | ☐ PASS ☐ FAIL | |
| 5 | 备份恢复 | ☐ PASS ☐ FAIL | |
| 6 | 在线更新 | ☐ PASS ☐ FAIL | |
| 7 | 更新回滚 | ☐ PASS ☐ FAIL | |
| 8 | Site Path 轮换 | ☐ PASS ☐ FAIL | |
| 9 | 自动更新启用/禁用 | ☐ PASS ☐ FAIL | |
| 10 | 卸载 | ☐ PASS ☐ FAIL | |
| 11 | 重新安装 | ☐ PASS ☐ FAIL | |
| 12 | Docker Caddy 模式（host 网络） | ☐ PASS ☐ FAIL | |
| 13 | Docker Caddy 模式（桥接网络） | ☐ PASS ☐ FAIL | |

**最终决策**： ☐ 批准发布 0.1.0  ☐ 需要修复后重测

**签字**： _______________  
**日期**： _______________
