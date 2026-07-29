# ProxyHub 生产部署指南

ProxyHub 内嵌 mihomo 代理内核,无需单独下载或配置。单二进制文件包含完整的前后端和代理内核。

## 前置条件

### 系统要求
- **操作系统**: Ubuntu 22.04/24.04 或 Debian 12/13
- **架构**: amd64 或 arm64
- **用户**: root 权限
- **systemd**: 必需（用于服务管理）
- **网络**: 出站 HTTPS 连接（用于下载和健康检查）

### 资源占用参考

容量规划参考值（单实例、常规节点规模）:

- **内存**: 50-128MB（闲置 / 健康检查期间）
- **CPU**: 1-5%（闲置）,10-20%（健康检查期间）
- **磁盘**: 二进制约 20MB（含前端 + mihomo 内核),数据库随节点与统计数据增长
- **并发健康检查**: 默认 30 个节点（可配置）
- **检查间隔**: 默认 15 分钟（可配置）

### 域名与 DNS
- 一个指向服务器的域名（如 `proxy.example.com`）
- DNS A/AAAA 记录已生效（安装前验证）
- TCP 80/443 端口可用或已被 Caddy 占用

### Caddy 反向代理
ProxyHub 仅监听 `127.0.0.1:8080`（环回地址），**必须**通过反向代理暴露 HTTPS 访问。

安装 Caddy v2（如果未安装）:
```bash
# Ubuntu/Debian
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt update
apt install caddy
```

验证 Caddy 运行状态:
```bash
systemctl status caddy
```

### 自带反向代理(--no-caddy)

如果服务器已有 nginx / Caddy / 其他反代占用 80/443，或你希望自己管理 TLS，用 `--no-caddy` 跳过安装器的 Caddy 配置:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh) \
  --non-interactive --domain proxy.example.com --no-caddy
```

该模式下安装器:不检查 80/443、不碰 Caddy、跳过公网 HTTPS 健康检查(回环健康仍验证),并在 `/etc/proxyhub/` 下生成两份可直接套用的反代样例:

- `reverse-proxy.caddy` — 与托管模式完全相同的 Caddy site block
- `reverse-proxy.nginx.conf` — nginx server 块模板

> **安全要点**:两份样例都强制**替换**(而非追加)`X-Forwarded-For`。ProxyHub 信任来自环回反代的 XFF 做 IP2Ban/验证码/黑名单判定,如果客户端自带的 XFF 能穿过反代,这些防护全部可绕过。套用配置时不要删掉这几行。

注意:`--no-caddy` 安装的实例,`proxyhubctl rotate-path` 不可用(它无法改写你自己的反代配置);换 Site Path 需手工同步你的反代配置与样例文件。

## 一键安装

### 交互式安装（推荐）

适合首次部署，安装器会引导您完成所有配置:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh)
```

安装器将:
1. 验证系统环境（OS、架构、systemd、网络）
2. 提示输入域名和可选的 ACME 邮箱
3. 自动生成安全的 Site Path（管理后台路径）
4. 生成高强度管理员密码（20 字符）
5. 下载并验证 ProxyHub 二进制文件（SHA256 校验）
6. 创建 `proxyhub` 系统用户和目录结构
7. 配置 systemd 服务并启动
8. 配置 Caddy 反向代理并申请 HTTPS 证书
9. 验证本地和公网健康检查端点
10. 输出管理员凭证和访问地址

**重要**: 安装完成后会显示管理员凭证，**请立即保存**:
```
========================================
ProxyHub 安装成功
========================================
管理后台: https://proxy.example.com/Kx9mY2vP3nQ8rW5tZ/
管理员账户: admin-7x4n
管理员密码: Jk9mN2pQ3vW8xZ5t1234
========================================
```

### 非交互式安装（CI/CD）

适合自动化部署，所有参数通过命令行传入:

```bash
bash install.sh \
  --non-interactive \
  --domain proxy.example.com \
  --email admin@example.com \
  --version latest
```

完整选项说明:
```bash
install.sh --help
```

### 安装后文件结构

```
/usr/local/bin/proxyhub          # 二进制文件（内嵌 mihomo 内核）
/usr/local/bin/proxyhubctl       # 运维 CLI
/etc/proxyhub/config.yaml        # 配置文件
/var/lib/proxyhub/               # 数据目录
├── data.db                      # SQLite 数据库
└── .state-fingerprint           # 状态指纹（验证升级未丢失数据）
/etc/systemd/system/proxyhub.service   # systemd 单元
/etc/caddy/conf.d/proxyhub.caddy       # Caddy 配置片段
/root/.proxyhub-install-info     # 安装记录（不含密码）
```

## 首次登录

1. 访问安装输出的管理后台 URL（包含 Site Path）
2. 使用生成的管理员凭证登录
3. 进入"机场管理"添加上游订阅
4. 进入"订阅地址"创建终端订阅
5. 将订阅地址复制到客户端（Clash/V2Ray/Shadowrocket）

## 数据备份

### 手动备份

```bash
proxyhubctl backup
```

生成加密归档到 `/var/lib/proxyhub/backups/proxyhub-backup-YYYYMMDD-HHMMSS.tar.gz.enc`

### 推荐备份策略

ProxyHub 数据库包含:
- 管理员密码（bcrypt 哈希）
- 机场订阅 URL（明文）
- 节点信息
- 自建节点配置（包含密码）

**建议**:
- 每日自动备份（cron）
- 备份归档已加密（使用 openssl enc + 基于 Site Path 的密钥）
- 异地保存备份（rsync 到备份服务器）
- 保留最近 7 天备份

示例 cron 任务（每日凌晨 2 点）:
```bash
0 2 * * * /usr/local/bin/proxyhubctl backup --protect && find /var/lib/proxyhub/backups -name "*.tar.gz.enc" -mtime +7 -delete
```

### 恢复备份

```bash
proxyhubctl restore /path/to/backup.tar.gz.enc --yes
```

恢复过程会:
1. 停止服务
2. 验证归档完整性
3. 解密并解压
4. 替换数据库
5. 验证状态指纹
6. 重启服务

## 更新升级

### 检查当前版本

```bash
proxyhub --version
```

### 在线更新（推荐）

```bash
proxyhubctl update
```

更新过程:
1. 自动备份当前数据
2. 下载最新版本并验证 SHA256
3. 停止服务
4. 替换二进制文件
5. 验证状态指纹（确保数据未丢失）
6. 重启服务
7. 验证健康检查端点

如果更新失败，自动回滚到旧版本。

### 指定版本更新

```bash
proxyhubctl update --version v0.2.0
```

### 自动更新（可选）

启用自动更新（每日检查）:
```bash
proxyhubctl auto-update enable
```

禁用自动更新:
```bash
proxyhubctl auto-update disable
```

## Site Path 轮换

Site Path 是管理后台的随机路径（如 `/Kx9mY2vP3nQ8rW5tZ/`）。轮换 Site Path 可降低扫描器噪音。

```bash
proxyhubctl rotate-path
```

轮换过程:
1. 生成新 Site Path
2. 更新 Caddy 配置
3. 重载 Caddy
4. 更新 `/root/.proxyhub-install-info`
5. 输出新的管理后台 URL

**注意**: 轮换后需使用新 URL 访问管理后台。旧 URL 立即失效。

## 卸载

完全移除 ProxyHub:

```bash
proxyhubctl uninstall
```

卸载过程会提示您是否保留数据备份。选择"是"将备份保存到 `/root/proxyhub-uninstall-backup-YYYYMMDD-HHMMSS.tar.gz.enc`。

## 运维操作

### 查看服务状态

```bash
proxyhubctl status
```

输出包含:
- 服务运行状态
- 进程 PID
- 内存占用
- 管理后台 URL
- 最后备份时间

### 查看日志

```bash
# 实时查看日志
proxyhubctl logs --follow

# 查看最近 100 行
proxyhubctl logs --lines 100
```

### 重启服务

```bash
proxyhubctl restart
```

### 查看安装信息

```bash
proxyhubctl show-info
```

输出包含:
- ProxyHub 版本
- 安装时间
- 域名
- Site Path
- 数据目录
- 管理员账户（不含密码）

## 性能调优

ProxyHub 内嵌 mihomo 内核用于健康检查（延迟测速和真实请求测试）。默认配置已优化，通常无需调整。

### 对于大量节点（200+ 个）

编辑 `/etc/proxyhub/config.yaml`:
```yaml
health_check:
  concurrent: 50        # 提高并发度（默认 30）
  timeout:
    latency: 3s         # 缩短超时（默认 5s）
    request: 5s         # 缩短超时（默认 10s）
```

重启服务:
```bash
proxyhubctl restart
```

### 对于低配服务器（<= 1GB RAM）

```yaml
health_check:
  concurrent: 10        # 降低并发度
  interval: 30m         # 延长检查间隔（默认 15m）
```

### 资源占用基准

- **内存**: 50-128 MB（闲置 / 健康检查期间）
- **CPU**: 1-5%（闲置）, 10-20%（健康检查期间）
- **磁盘**: 数据库增长约 1MB/千次订阅拉取

## 监控与告警

### 健康检查端点

ProxyHub 提供健康检查端点，用于监控系统（Prometheus、Uptime Kuma 等）:

```bash
# 本地检查
curl http://127.0.0.1:8080/<site-path>/health

# 公网检查（通过 Caddy）
curl https://proxy.example.com/<site-path>/health
```

返回 `200 OK` 表示服务正常。

### 飞书告警（可选）

ProxyHub 支持飞书 Webhook 告警:
- 机场失效通知
- 可用节点不足告警
- 系统错误通知

在管理后台"系统设置"中配置飞书 Webhook URL。

### 推荐监控指标

- `/health` 端点可用性（每分钟检查）
- 数据库文件大小（每日检查）
- 进程资源占用（每 5 分钟）
- 备份成功率（每日检查 `/var/lib/proxyhub/.last-backup`）

## 常见问题

详见 [FAQ.md](FAQ.md)。

## 安全加固

详见 [SECURITY.md](./SECURITY.md)。
