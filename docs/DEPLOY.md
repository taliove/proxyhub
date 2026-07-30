# ProxyHub 生产部署指南

ProxyHub 内嵌 mihomo 代理内核，无需单独下载或配置。单二进制文件包含完整的前后端和代理内核。

## 前置条件

### 系统要求
- **操作系统**： Ubuntu 22.04/24.04 或 Debian 12/13
- **架构**： amd64 或 arm64
- **用户**： root 权限
- **systemd**: 必需（用于服务管理）
- **网络**： 出站 HTTPS 连接（用于下载和健康检查）
- **Docker**： 仅 Docker Caddy 模式需要——Docker ≥ 20.10，且安装器以 root 直接执行 docker 命令（详见「Docker 容器中的 Caddy」）

### 资源占用参考

容量规划参考值（单实例、常规节点规模）：

- **内存**： 50-128MB（闲置 / 健康检查期间）
- **CPU**: 1-5%（闲置），10-20%（健康检查期间）
- **磁盘**： 二进制约 20MB（含前端 + mihomo 内核），数据库随节点与统计数据增长
- **并发健康检查**： 默认 30 个节点（可配置）
- **检查间隔**： 默认 15 分钟（可配置）

### 域名与 DNS
- 一个指向服务器的域名（如 `proxy.example.com`）
- DNS A/AAAA 记录已生效（安装前验证）
- TCP 80/443 端口可用或已被 Caddy 占用

### Caddy 反向代理
ProxyHub 的管理面只监听本机地址（默认 `127.0.0.1:8080` 环回；Docker 桥接拓扑例外，见「Docker 容器中的 Caddy」），**必须**通过反向代理暴露 HTTPS 访问。

安装器识别三种 Caddy 形态：宿主机原生 systemd Caddy（native，本节）、Docker 容器中的 Caddy（docker，见下节）、运维自带反代（`--no-caddy`，none）。

安装 Caddy v2（如果未安装）：
```bash
# Ubuntu/Debian
apt install -y debian-keyring debian-archive-keyring apt-transport-https
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt update
apt install caddy
```

验证 Caddy 运行状态：
```bash
systemctl status caddy
```

### Docker 容器中的 Caddy(--caddy-docker)

适用场景：目标机的 Caddy v2 已经跑在 Docker 容器里（`docker run` 或 compose 部署），宿主机上没有原生 caddy 二进制。安装器识别并集成这个容器：写配置片段、容器内校验与重载、失败自动回滚，ProxyHub 本体仍是 systemd 裸机安装——本模式不是全容器化编排。

#### 前提

- **Docker ≥ 20.10**：桥接网络的容器依赖 `host.docker.internal:host-gateway` 映射能力（20.10 引入）；
- **root 直接执行 docker 命令**：安装器本身以 root 运行，对 Docker 的调用由 root 直接发起，需能访问 Docker socket；
- **镜像为 caddy**：镜像名取最后一段（去掉 registry/命名空间前缀）、再去掉 tag/digest 后恰为 `caddy`(`caddy`、`caddy:2`、`registry.example.com/caddy:2`、`team/caddy` 均可；`caddy-fork`、`team/caddy-proxy` 之类不算)。

#### 容器要求清单

安装器逐项校验，任一不满足即拒绝安装（fail closed）并打印修复指引：

1. 容器存在且处于运行中；
2. `/etc/caddy` 是持久挂载：bind mount 或 named volume 均可，安装器解析到宿主机路径写入配置。**单文件挂载**（如只挂 Caddyfile）或无挂载会被拒绝——写在容器层上的配置重建即丢；
3. 桥接网络的容器：已发布 TCP 80 与 443（host 网络容器豁免，它直接绑定宿主机端口）；
4. 桥接网络的容器：具备 `host.docker.internal:host-gateway` 映射（`docker run` 加 `--add-host host.docker.internal:host-gateway`,compose 用 `extra_hosts`）。

#### 模式优先级与自动探测

安装器按以下顺序判定 Caddy 模式（`--caddy-docker` 与 `--no-caddy` 互斥，同给即报错退出）：

1. 给了 `--caddy-docker <容器名>`：强制 docker 模式，校验该容器；
2. 宿主机存在 caddy 二进制：走 native（现状不变）；
3. 恰好一个运行中的 caddy 镜像容器：自动选用，并在安装日志中明示；
4. 零个或多个候选：报错退出（多个时列出候选名，要求显式指名）。

#### 用法示例

docker run 形态（桥接网络）：

```bash
docker run -d --name caddy \
  -p 80:80 -p 443:443 \
  -v /srv/caddy:/etc/caddy \
  --add-host host.docker.internal:host-gateway \
  caddy:2

bash install.sh --non-interactive --domain proxy.example.com \
  --caddy-docker caddy
```

compose 形态（等价片段）：

```yaml
services:
  caddy:
    image: caddy:2
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /srv/caddy:/etc/caddy
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

host 网络容器（`network_mode: host`）无需任何额外参数：80/443 发布检查豁免，回环拓扑原样保留，行为与 native 一致。

#### 桥接模式的安全降级（务必知情）

> **警告**：桥接网络的 caddy 容器够不到宿主机的回环地址，安装器因此把 ProxyHub 管理面绑定到该 docker 网桥的网关 IP，并把 `trusted_proxies` 收窄到该网桥子网。这意味着管理面的信任边界从「仅本机 loopback」扩到「整个该 docker 网桥」：**同一网桥上的任意容器都可以直连管理面（无 TLS），且可以伪造 X-Forwarded-For 绕过 IP 层防御**（IP2Ban、验证码、黑名单的判定都可能被欺骗）。此外，桥接上容器到管理面的回连流量是**明文 HTTP**，同网桥容器可被动嗅探到 Site Path——Site Path 保密这一层对网桥内的嗅探者不成立，但反代层的 XFF 替换仍然有效。安装摘要会再次打印这条警告。
>
> 不信任网桥内其他容器的部署，请改用以下任一方案：
> - host 网络的 caddy 容器（信任边界不扩）；
> - 原生 systemd Caddy(native 模式)；
> - 把 caddy 隔离在独立的 docker 网桥上，网桥内只放你信任的容器。

#### 安装后的运维对齐

安装档案（`/root/.proxyhub-install-info`）新增 `CADDY_MODE` 与 `CADDY_CONTAINER` 两个字段，`proxyhubctl` 的 update / backup / restore / rotate-path / uninstall 读取档案后自动走 docker 通道（容器内 fmt/validate/reload,fragment 写到容器挂载的宿主机路径），无需额外参数。容器失联（被删、被改名）时这些命令 fail closed 并明示；容器换了名字时，编辑档案里的 `CADDY_CONTAINER` 为新容器名即可。Caddy admin API 被禁用（`admin off`）时，重载自动 fallback 为 `docker restart <容器>`，并警告对其他站点的短暂中断。

### 自带反向代理(--no-caddy)

如果服务器已有 nginx / Caddy / 其他反代占用 80/443，或你希望自己管理 TLS，用 `--no-caddy` 跳过安装器的 Caddy 配置：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh) \
  --non-interactive --domain proxy.example.com --no-caddy
```

该模式下安装器：不检查 80/443、不碰 Caddy、跳过公网 HTTPS 健康检查（回环健康仍验证），并在 `/etc/proxyhub/` 下生成两份可直接套用的反代样例：

- `reverse-proxy.caddy` — 与托管模式完全相同的 Caddy site block
- `reverse-proxy.nginx.conf` — nginx server 块模板

> **安全要点**：两份样例都强制**替换**（而非追加）`X-Forwarded-For`。ProxyHub 信任来自环回反代的 XFF 做 IP2Ban/验证码/黑名单判定，如果客户端自带的 XFF 能穿过反代，这些防护全部可绕过。套用配置时不要删掉这几行。

注意：`--no-caddy` 安装的实例，`proxyhubctl rotate-path` 不可用（它无法改写你自己的反代配置）；换 Site Path 需手工同步你的反代配置与样例文件。

## 国内部署（网络受限环境）

GitHub（raw.githubusercontent.com、github.com/releases）在国内大面积不可达，默认安装入口与制品下载都会卡住。本节给出完整替代路径：入口脚本走 jsDelivr CDN，制品下载走 `--download-base` 指向的镜像。安全性不依赖镜像可信，见下文「签名信任锚」。

### 入口：jsDelivr

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/taliove/proxyhub@main/install.sh | bash
```

安装器运行期拉取伴侣库（lib.sh、proxyhubctl）同样 jsDelivr 优先、raw.githubusercontent 后备，两者都失败才报错退出。注意：jsDelivr 只解决「脚本入口」，制品（tarball）默认仍从 GitHub releases 下载——GitHub 整体不可达的机器还必须配合下面的镜像下载基。另请注意 jsDelivr 的 `@main` 缓存最长约 12 小时：刚发布的版本（尤其公钥轮换后的首个版本）经 jsDelivr 入口可能拿到旧脚本，此时改用 raw.githubusercontent 入口或等缓存过期。

### 制品走镜像：--download-base

`--download-base URL`（或环境变量 `PROXYHUB_DOWNLOAD_BASE`，旗标优先）覆盖制品下载基，制品 URL 按 `<base>/<version>/<asset>` 解析，URL 必须是 https://。默认值永远是 GitHub 官方 releases，不内置任何第三方镜像。镜像模式下 latest 自动解析不可用（它依赖 GitHub 重定向），**必须显式 `--version`**，否则安装器直接拒绝并提示；下载基不可达时报错并给出指引，无静默回退。

配方（镜像 + 显式版本）：

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/taliove/proxyhub@main/install.sh -o install.sh
bash install.sh --non-interactive \
  --domain proxy.example.com \
  --version 0.3.0 \
  --download-base https://<镜像>/taliove/proxyhub/releases/download
```

`--version` 取一个已发布的版本号（去 releases 页或 CHANGELOG 挑；镜像转发的是同一批 release 资产）。

**镜像从哪来**：前缀转发型 ghproxy 类公共服务是当前常见形态——下载基写成「镜像域名前缀 + 原始 GitHub 路径」的拼接，形如 `https://ghproxy.example.com/https://github.com/taliove/proxyhub/releases/download`；自建 nginx 反代 `https://github.com/taliove/proxyhub/releases/download/` 是更稳定的选项。**任何第三方镜像都会过期，本仓库不担保任何具体镜像的可用性，以你实际可达为准**。自查方法：直接探测该镜像能否取到完整制品——

```bash
curl -fsSI https://<镜像>/<version>/SHA256SUMS.minisig
```

签名文件（.minisig）与 tarball 都能拿到才算完整转发；只转发热门大文件、丢掉小签名文件的镜像，安装器会 fail closed（见下节）。

### 为什么镜像下载也安全（签名信任锚）

release 资产除 tarball 与 SHA256SUMS 外还包含 `SHA256SUMS.minisig`——发布方用私钥对 SHA256SUMS 的数字签名。安装器与 `proxyhubctl update` 内嵌对应公钥，下载后先用 openssl 验签，再按验过的 SHA256SUMS 核校 tarball；签名缺失、格式非法或验签失败一律 fail closed，拒绝安装。信任链是「内嵌公钥 → 签名 → 校验和 → 制品」，与下载通道无关：恶意镜像可以同时替换 tarball 和 SHA256SUMS，但伪造不出能通过内嵌公钥验签的签名。验签只需要 openssl（≥ 1.1.1，Ubuntu 22.04+/Debian 12+ 自带），不引入新依赖。决策细节见 [ADR 0036](adr/0036-artifact-signing-and-mirror-channel.md)。

### 更新沿用下载基

安装时生效的下载基写入安装档案（`/root/.proxyhub-install-info` 的 `DOWNLOAD_BASE=` 字段），`proxyhubctl update` 自动沿用，升级不必重记镜像参数；显式 `--download-base` 优先于档案值，是换镜像时的确定性覆盖手段。镜像模式下 update 同样需要显式 `--version`。

> **注意**：镜像模式的安装不宜启用自动更新（`proxyhubctl auto-update enable`）——自动更新不带显式版本，按镜像版本纪律会被拒绝；请定期手动 `proxyhubctl update --version <版本>`。

### Caddy 镜像：Docker 加速器

caddy 容器镜像拉自 Docker Hub，国内同样受限。给 Docker 配 registry mirror 后重拉：

```json
// /etc/docker/daemon.json
{
  "registry-mirrors": ["https://<你的加速器地址>"]
}
```

```bash
systemctl restart docker
docker pull caddy:2
```

加速器地址由你的云厂商控制台或所用镜像服务提供（阿里云、腾讯云等为每个账号分配专属地址），同样以实际可达为准。宿主原生 Caddy（Cloudsmith 仓库）国内一般可达；不可达时改用 Docker Caddy 模式（见「Docker 容器中的 Caddy」）。

### GeoIP 库

订阅拉取地域白名单依赖的 GeoIP 库，更新源 `download.db-ip.com` 国内基本可达，通常无需处理；确有障碍时 `scripts/geoip/update.sh` 支持 `GEOIP_BASE_URL` 覆盖下载基。

## 一键安装

### 交互式安装（推荐）

适合首次部署，安装器会引导您完成所有配置：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh)
```

安装器将：
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

**重要**： 安装完成后会显示管理员凭证，**请立即保存**：
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

适合自动化部署，所有参数通过命令行传入：

```bash
bash install.sh \
  --non-interactive \
  --domain proxy.example.com \
  --email admin@example.com \
  --version latest
```

完整选项说明：
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
/etc/caddy/conf.d/proxyhub.caddy       # Caddy 配置片段(docker 模式:容器 /etc/caddy 挂载的宿主机路径下)
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

ProxyHub 数据库包含：
- 管理员密码（bcrypt 哈希）
- 机场订阅 URL（明文）
- 节点信息
- 自建节点配置（包含密码）

**建议**：
- 每日自动备份（cron）
- 备份归档已加密（使用 openssl enc + 基于 Site Path 的密钥）
- 异地保存备份（rsync 到备份服务器）
- 保留最近 7 天备份

示例 cron 任务（每日凌晨 2 点）：
```bash
0 2 * * * /usr/local/bin/proxyhubctl backup --protect && find /var/lib/proxyhub/backups -name "*.tar.gz.enc" -mtime +7 -delete
```

### 恢复备份

```bash
proxyhubctl restore /path/to/backup.tar.gz.enc --yes
```

恢复过程会：
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

更新过程：
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

启用自动更新（每日检查）：
```bash
proxyhubctl auto-update enable
```

禁用自动更新：
```bash
proxyhubctl auto-update disable
```

## Site Path 轮换

Site Path 是管理后台的随机路径（如 `/Kx9mY2vP3nQ8rW5tZ/`）。轮换 Site Path 可降低扫描器噪音。

```bash
proxyhubctl rotate-path
```

轮换过程：
1. 生成新 Site Path
2. 更新 Caddy 配置
3. 重载 Caddy
4. 更新 `/root/.proxyhub-install-info`
5. 输出新的管理后台 URL

**注意**： 轮换后需使用新 URL 访问管理后台。旧 URL 立即失效。

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

输出包含：
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

输出包含：
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

重启服务：
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

- **内存**： 50-128 MB（闲置 / 健康检查期间）
- **CPU**: 1-5%（闲置）， 10-20%（健康检查期间）
- **磁盘**： 数据库增长约 1MB/千次订阅拉取

## 监控与告警

### 健康检查端点

ProxyHub 提供健康检查端点，用于监控系统（Prometheus、Uptime Kuma 等）：

```bash
# 本地检查
curl http://127.0.0.1:8080/<site-path>/health

# 公网检查（通过 Caddy）
curl https://proxy.example.com/<site-path>/health
```

返回 `200 OK` 表示服务正常。

### 飞书告警（可选）

ProxyHub 支持飞书 Webhook 告警：
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
