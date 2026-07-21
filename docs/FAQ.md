# 常见问题(FAQ)

面向 ProxyHub 使用者的常见问题与解答。部署运维细节见 [DEPLOY.md](DEPLOY.md),安全模型见 [SECURITY.md](SECURITY.md)。

## 安装与账户

### Q: 忘记/重置管理员密码怎么办?

A: 安装器生成的账户信息保存在 `/root/.proxyhub-install-info`(仅包含账户名,不含密码)。如果密码丢失:

```bash
# 方案1: 恢复备份
proxyhubctl restore /var/lib/proxyhub/backups/latest-backup.tar.gz.enc --yes

# 方案2: 手动重置(需停止服务并修改数据库)
systemctl stop proxyhub
sqlite3 /var/lib/proxyhub/data.db "UPDATE settings SET value='<new-bcrypt-hash>' WHERE key='admin_password';"
systemctl start proxyhub
```

开发环境可直接删除 `var/data/data.db` 重新初始化。

### Q: 如何更换域名?

A: 需要更新 Caddy 配置和 DNS:

```bash
# 1. 编辑 Caddy 配置
vim /etc/caddy/conf.d/proxyhub.caddy
# 修改 proxy.example.com 为新域名

# 2. 重载 Caddy
systemctl reload caddy

# 3. 更新 DNS A/AAAA 记录指向服务器 IP
```

### Q: 端口冲突怎么办?

A: ProxyHub 监听 `127.0.0.1:8080`(环回地址)。如果冲突,编辑 `/etc/proxyhub/config.yaml`:

```yaml
server:
  listen: "127.0.0.1:8081"  # 修改端口
```

同时更新 Caddy 配置 `/etc/caddy/conf.d/proxyhub.caddy` 中的 `reverse_proxy` 目标端口。

## 订阅与节点

### Q: 支持哪些代理协议?

A: VMess、VLess、Trojan、Shadowsocks(通过内嵌 mihomo 内核支持)。

### Q: 如何添加自建节点?

A: 在管理后台"系统设置"中配置自建节点信息。自建节点可作为 FailBack,与机场节点统一管理。

### Q: 订阅地址拉取失败 "no available nodes"?

A: 等待首次健康检查完成(启动后 15 分钟内)。查看日志:

```bash
proxyhubctl logs --lines 100 | grep health
```

## 代理内核

### Q: 内嵌的代理内核如何更新?

A: mihomo 内核内嵌在 ProxyHub 二进制中。运行 `proxyhubctl update` 会同时更新 ProxyHub 与内核,无需单独更新。

### Q: 如何调整健康检查行为?

A: 内核配置由 ProxyHub 内部生成,无需也不应手动修改。如需调整健康检查行为,编辑 `/etc/proxyhub/config.yaml` 中的 `health_check` 部分。

## 获取帮助

遇到未覆盖的问题,请提供:
1. 完整日志(`proxyhubctl logs --lines 500`)
2. 系统信息(`proxyhubctl show-info`)
3. 错误截图或描述

GitHub Issues: https://github.com/taliove/proxyhub/issues
