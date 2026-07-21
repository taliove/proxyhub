# ProxyHub 安全模型

本文档说明 ProxyHub 的安全设计、威胁模型、防护措施和假设前提。

## 架构安全

### 环回监听（Loopback-only Listener）

ProxyHub 仅监听 `127.0.0.1:8080`（环回地址），**不直接暴露到公网**。所有外部访问必须通过 Caddy 反向代理。

**防护目标**:
- 防止未加密 HTTP 流量暴露
- 防止绕过 Caddy 的访问控制
- 降低直接攻击面

**验证**:
```bash
# 应返回连接拒绝或超时
curl http://<服务器公网IP>:8080/<site-path>/health

# 应通过 Caddy 返回 200
curl https://proxy.example.com/<site-path>/health
```

### Caddy TLS 终止

ProxyHub 不实现 TLS。所有 HTTPS 流量由 Caddy 终止，Caddy 自动管理 Let's Encrypt 证书。

**防护目标**:
- 强制加密传输（订阅地址包含敏感节点信息）
- 自动证书续期（避免人为失误）
- HTTP/2 和现代 TLS 协议支持

**假设前提**:
- Caddy 配置正确（由安装器自动生成）
- TCP 80/443 端口对外开放（Let's Encrypt ACME 挑战）
- 域名 DNS 记录正确指向服务器

### 无代理内核端口暴露

ProxyHub 内嵌 mihomo 作为代理内核,仅用于健康检查(延迟测速和真实请求测试)。内核以拨号方式发起出站连接,不监听任何端口,不接受外部连接,不用于流量代理。

**防护目标**:
- 避免代理内核端口被扫描器发现
- 降低攻击面

**验证**:
```bash
# 不应有代理内核相关端口监听(仅管理面 loopback 端口)
netstat -tulpn | grep proxyhub
```

ProxyHub 是**订阅聚合器**，不是流量代理。终端设备直接连接机场节点，不经过 ProxyHub。

## 认证与授权

### Site Path 随机化

管理后台路径（Site Path）在安装时随机生成（20-64 字符，包含大小写字母、数字、下划线、连字符，至少 3 种字符类型）。

**示例**: `/Kx9mY2vP3nQ8rW5tZ/`

**防护目标**:
- 降低自动化扫描器噪音（不是 `/admin/` `/login/` 等常见路径）
- 减少日志污染（登录失败尝试）
- 提高暴力破解成本（需先猜出路径）

**非目标**:
- **不是密钥**: Site Path 会写入 Caddy 配置、systemd 日志、访问日志，不应视为机密
- **不替代认证**: 即使知道 Site Path，仍需管理员凭证登录

**轮换**: 定期轮换 Site Path 进一步降低暴露风险
```bash
proxyhubctl rotate-path
```

### 管理员密码强度

安装器自动生成 20 字符高强度密码（大小写字母 + 数字 + 特殊字符）。

**防护目标**:
- 防止弱密码暴力破解
- 防止字典攻击
- 满足 NIST SP 800-63B 密码指南

**存储**: bcrypt 哈希（cost factor 10），存储在 SQLite 数据库中。

**建议**:
- 使用密码管理器保存（1Password、Bitwarden）
- 定期轮换密码（每 90 天）
- 不在不可信设备上使用

### IP2Ban 防暴力破解

ProxyHub 内置 IP2Ban 机制:
- 连续 5 次登录失败 → 封禁 IP 1 小时
- 封禁期间该 IP 的所有请求返回 403

**防护目标**:
- 防止自动化暴力破解
- 降低日志噪音

**配置**: 在管理后台"系统设置"中调整阈值和封禁时长。

**误封解除**:
```bash
# 查看当前封禁 IP
sqlite3 /var/lib/proxyhub/data.db "SELECT ip, ban_count, first_attempt, ban_until FROM banned_ips;"

# 手动解封
sqlite3 /var/lib/proxyhub/data.db "DELETE FROM banned_ips WHERE ip='<IP地址>';"
```

### 会话管理

- 会话 Cookie 设置 `HttpOnly` 和 `Secure` 标志
- 会话超时 24 小时（无活动自动登出）
- 会话密钥从环境变量或配置文件读取（不硬编码）

## 数据安全

### 敏感数据清单

ProxyHub 存储以下敏感数据:

| 数据类型 | 存储位置 | 加密状态 | 风险 |
|---------|---------|---------|------|
| 管理员密码 | `data.db` | bcrypt 哈希 | 低（需彩虹表 + 高算力） |
| 机场订阅 URL | `data.db` | 明文 | **高**（泄露可拉取节点） |
| 自建节点配置 | `data.db` | 明文 | **高**（含密码/UUID） |
| 订阅地址 Token | `data.db` | 明文 | 中（泄露可拉取聚合订阅） |
| Site Path | `data.db` + Caddy 配置 | 明文 | 低（知道路径仍需密码） |
| 备份归档 | `/var/lib/proxyhub/backups/` | **加密**（openssl enc） | 低（需 Site Path 派生密钥） |

### 备份加密

`proxyhubctl backup` 自动使用 openssl enc 加密备份归档:
- 密钥派生自 Site Path（PBKDF2 + 随机 salt）
- AES-256-CBC 加密
- 归档包含完整数据库和配置文件

**恢复需要**:
- 备份归档文件
- 当前系统的 Site Path（从 `/root/.proxyhub-install-info` 读取）

**风险**: 如果攻击者同时获得备份归档和 Site Path，可解密备份。

**建议**:
- 备份归档异地存储（rsync 到备份服务器）
- 备份服务器不存储 Site Path
- 使用独立密钥二次加密备份（在备份服务器端）

### 状态指纹验证

ProxyHub 在升级和恢复时验证"状态指纹"（retained-state fingerprint）:
- 指纹包含数据库表结构和关键设置的 HMAC-SHA256 摘要
- 升级后验证指纹，确保数据未丢失或损坏
- 恢复备份后验证指纹，确保备份完整

**防护目标**:
- 防止升级脚本错误导致的数据丢失
- 检测备份归档损坏
- 提供回滚依据

**实现**: `/var/lib/proxyhub/.state-fingerprint` 文件存储最后验证的指纹。

## 网络安全

### 出站 HTTPS 验证

ProxyHub 拉取机场订阅和健康检查时，**强制验证 TLS 证书**。不允许禁用证书验证（no `--insecure` flag）。

**防护目标**:
- 防止中间人攻击（MITM）
- 防止 DNS 劫持
- 确保拉取的节点数据完整性

**假设前提**:
- 系统 CA 证书库是最新的（`ca-certificates` 包）
- 机场订阅 URL 使用有效 HTTPS 证书

### DNS 隐私

ProxyHub 使用系统默认 DNS 解析器。如需 DNS 隐私保护（DoH/DoT），需在操作系统层面配置。

**建议**:
- 使用 systemd-resolved 配置 DoH（Ubuntu 22.04+）
- 或使用 cloudflared 代理（Cloudflare DNS over HTTPS）

### 防火墙配置（可选）

ProxyHub 默认配置已足够安全（环回监听 + Caddy TLS）。如需额外防护:

```bash
# UFW 示例：仅开放 SSH + HTTP/HTTPS
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw enable
```

## 假设前提（Operator Responsibilities）

ProxyHub 的安全模型依赖运维人员保障以下前提:

### 主机安全
- **操作系统**: 及时安装安全补丁（`apt update && apt upgrade`）
- **SSH**: 禁用密码登录，仅允许密钥认证
- **防火墙**: 仅开放必要端口（80/443/SSH）
- **用户权限**: 不在 root 用户下运行非必需服务
- **物理访问**: 机房/VPS 控制台访问权限受控

### Caddy 配置
- **不篡改**: 不手动编辑 `/etc/caddy/conf.d/proxyhub.caddy`（由 `install.sh` 和 `proxyhubctl rotate-path` 管理）
- **不禁用 HTTPS**: 不移除 Caddy 自动 HTTPS 配置
- **IP 白名单（可选）**: 在 Caddy 配置中限制管理后台访问 IP

```caddy
# 示例：仅允许办公网 IP 访问管理后台
proxy.example.com {
    @admin {
        path /<site-path>/*
    }
    handle @admin {
        @allowed {
            remote_ip 203.0.113.0/24
        }
        reverse_proxy @allowed 127.0.0.1:8080
        respond 403
    }
    reverse_proxy 127.0.0.1:8080
}
```

### 备份安全
- **异地存储**: 备份归档不保存在同一服务器
- **访问控制**: 备份服务器权限受控
- **二次加密（建议）**: 在备份服务器端二次加密归档

### 密钥管理
- **管理员凭证**: 使用密码管理器，不在明文文件中保存
- **SSH 密钥**: 使用 passphrase 保护私钥
- **机场订阅 URL**: 妥善保管，不泄露给第三方

## 威胁模型

### 防护场景（In Scope）

✅ **自动化扫描器**: Site Path 随机化降低扫描成本  
✅ **暴力破解**: IP2Ban + 高强度密码  
✅ **中间人攻击**: Caddy TLS + 出站 HTTPS 验证  
✅ **数据泄露**: 备份加密 + bcrypt 密码哈希  
✅ **配置错误**: 安装器自动化 + 健康检查验证  

### 非防护场景（Out of Scope）

❌ **主机沦陷**: 如果攻击者获得 root 权限，可读取所有数据（包括解密备份）  
❌ **Caddy 漏洞**: 依赖 Caddy 项目的安全响应  
❌ **供应链攻击**: 依赖 Go 模块和 mihomo 内核的安全性  
❌ **社会工程**: 依赖运维人员的安全意识  
❌ **物理访问**: 依赖机房安全和 VPS 提供商  

### 边界条件

- **DNS 劫持**: 如果攻击者控制 DNS 解析，可拦截机场订阅拉取。缓解措施: 使用 DNSSEC 或 DoH。
- **Let's Encrypt 帐号接管**: 如果攻击者获得 ACME 帐号控制权，可签发假证书。缓解措施: 监控证书透明日志（CT logs）。
- **数据库损坏**: 如果 SQLite 数据库损坏，需从备份恢复。缓解措施: 每日自动备份。

## 安全响应流程

### 发现漏洞

如果您发现 ProxyHub 的安全漏洞，请**不要公开披露**。通过以下方式私下报告:

- **GitHub Security Advisory**: https://github.com/taliove/proxyhub/security/advisories/new
- **Email**: security@example.com（加密 PGP: `ABCD1234`）

### 响应时间

- 确认收到报告: 48 小时内
- 初步评估: 7 天内
- 修复发布: 根据严重程度（Critical: 14 天, High: 30 天）

### 公开披露

修复发布后 90 天，或在修复后协商公开。漏洞报告者将在 CHANGELOG 和 SECURITY.md 中获得署名。

## 安全检查清单

### 安装后检查

- [ ] ProxyHub 仅监听 `127.0.0.1:8080`（`netstat -tulpn | grep 8080`）
- [ ] Caddy HTTPS 证书有效（访问 `https://<domain>/<site-path>/health`）
- [ ] 管理员凭证已保存到密码管理器
- [ ] Site Path 已记录（`/root/.proxyhub-install-info`）
- [ ] 备份计划已配置（cron）
- [ ] 防火墙仅开放 22/80/443
- [ ] SSH 密码登录已禁用

### 定期检查（每季度）

- [ ] 操作系统补丁已更新
- [ ] ProxyHub 已更新到最新版本（`proxyhubctl update`）
- [ ] 备份归档可成功恢复（测试 `proxyhubctl restore`）
- [ ] 封禁 IP 列表已清理（移除误封）
- [ ] 审计日志已归档（`audit_logs` 表保留 90 天）
- [ ] 管理员密码已轮换

### 安全事件响应

- [ ] 立即轮换 Site Path（`proxyhubctl rotate-path`）
- [ ] 轮换管理员密码
- [ ] 检查审计日志（`SELECT * FROM audit_logs WHERE success=0 ORDER BY timestamp DESC LIMIT 100;`）
- [ ] 检查封禁 IP（`SELECT * FROM banned_ips ORDER BY ban_until DESC;`）
- [ ] 恢复最近备份（验证数据完整性）
- [ ] 升级到最新版本（如有安全补丁）

## 合规性（可选）

ProxyHub 设计时未针对特定合规框架（GDPR、HIPAA、SOC 2），但提供以下安全控制:

- **访问控制**: 管理员认证 + IP2Ban
- **加密**: TLS 传输 + 备份加密
- **审计**: 登录日志（90 天）
- **备份**: 自动化备份 + 恢复验证
- **密码管理**: bcrypt 哈希 + 高强度生成

如需满足特定合规要求，可能需要额外配置（如日志转发到 SIEM、定期渗透测试）。

## 参考资源

- [OWASP Top Ten](https://owasp.org/www-project-top-ten/)
- [NIST SP 800-63B: Digital Identity Guidelines](https://pages.nist.gov/800-63-3/sp800-63b.html)
- [Caddy Security Best Practices](https://caddyserver.com/docs/conventions#security)
- [mihomo Security](https://github.com/MetaCubeX/mihomo/security)

---

**最后更新**: 2026-07-19  
**版本**: 0.1.0-rc.1
