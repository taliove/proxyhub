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

- 会话 Cookie 设置 `HttpOnly` 与 `SameSite=Strict`（TLS 由 Caddy 终止,ProxyHub 自身只监听回环,故不设 `Secure` 标志）
- 会话有效期 12 小时,过期后需重新登录
- 会话保存在服务进程内存中,重启即全部失效(无持久化会话表)

### 登录加固:验证码

管理面登录在 IP2Ban 之前多了一层**软拦截**:封禁是拒绝服务(硬),验证码只是抬高自动化成本(软,不误伤人)。

**触发机制**:同一 IP 的历史登录失败次数(判定依据 `banned_ips.fail_count`)达到超管专属设置 `captcha_trigger_threshold` 即要求验证码,默认 **1**(一次失败就上码);设为 `0` 表示常驻要求。无失败记录视为 0 次。

**验证码规格**(自托管,不依赖第三方):

| 项 | 取值 |
|---|---|
| 长度与字符集 | 6 位,`ABCDEFGHJKMNPQRSTUVWXYZ23456789`(剔除易混淆的 0/O/1/I/l) |
| 干扰 | 6 个噪点字符 + 空心线/黏液线/正弦线三族干扰线 |
| 有效期 | 5 分钟 |
| 使用次数 | 一次性(答对即销毁;答错保留原图,允许改正手误) |
| 签发节流 | 每 IP 每分钟最多 30 张,超出 `GET /api/captcha` 回 429 |

**判定顺序(硬约束)**:封禁检查 → 蜜罐检查 → **验证码闸门** → 密码校验 → MFA 分流。验证码在密码之前,爆破者拿不到人机验证就碰不到 bcrypt。

**失败口径**:验证码缺失或答错与密码错**同一个计数器**,同阈值触发 IP2Ban,审计事件 `captcha_failure`(detail 区分"缺少验证码"与"验证码校验失败")。

**豁免边界**(与封禁/蜜罐一致):来源 IP 恰为 `127.0.0.1` 时豁免验证码。仅作用于 `POST /api/login`,不覆盖初始化向导(`/api/setup`)与第二段登录(`/api/login/mfa`,其凭据是 IP 绑定且有失败预算的 pending token)。

**配置**:`captcha_trigger_threshold` 是超管专属设置键,当前未出现在设置页表单,通过 `PUT /api/settings`(超管全局视图)或直接改 `system_settings` 表调整。

### 登录加固:多因素认证(MFA)

**强制范围**:TOTP 对**所有账号强制**,无豁免角色。未绑定认证器的会话在 `requireMFAEnrolled` 处被拒(403 + `must_enroll_mfa`),只放行绑定页自身所需的四条路径:`/api/me`、`/api/me/password`、`/api/me/mfa/enroll`、`/api/logout`(精确路径匹配,不做前缀匹配)。绑定状态每个请求都重读 `users` 表,所以超管重置后当前活跃会话立刻被重新关上。

**两段式绑定**:第一段只**暂存**密钥(`totp_enabled=0`,登录路径不看未启用的密钥,所以暂存态是惰性的);第二段提交一个能对上暂存密钥的验证码,才置 `totp_enabled=1` 并**一次性**返回 10 个恢复码。扫错 App 或中途放弃的用户不会落进"要求验证码但拿不出验证码"的死锁。已绑定账号不能自助重绑(409),换绑走管理员/CLI 重置。

**两段式登录**:密码正确后按三条分支收口(顺序是硬约束):

1. **未绑定** → 直接发会话,载荷带 `must_enroll_mfa`(没有第二因子可挑战);绑定检查必须先于受信判定,否则一条陈旧受信记录会放过"该绑定却没绑定"的账号。
2. **已绑定 + 该 (账号, IP) 有活跃受信记录** → 直接发会话并续期受信窗口,审计 `login_success` 带 `mfa_skipped=trusted_ip`。
3. **已绑定 + 未受信** → 只回 `{mfa_required:true, mfa_pending_token}`,不发会话;第二段 `POST /api/login/mfa` 用该 token 换会话。

`mfa_pending` 握手的约束:绑定单一账号 + 单一来源 IP、TTL 5 分钟、最多 5 次错码(超出即销毁)、兑换一次性(并发提交只产出一个会话)。每次错码同时扣 pending 预算与**每 IP 登录失败计数**(与密码错同阈值),所以攻击者无法靠重跑密码段买到无限次尝试。审计事件 `mfa_failure`,detail 只留 pending token 前 8 位便于关联,不落完整凭据。

**已知边界**:`POST /api/login/mfa` 自身不做封禁检查。封禁只掐断"再取新握手",已在飞行中的握手(持有者已证明密码)仍可在其 TTL/预算内兑换。想立即掐断在飞行的握手,重启服务即可(pending 全在内存)。

**时钟同步**:TOTP 步长 30 秒,校验容忍前后各 1 步(即约 ±30 秒漂移)。服务器时钟漂移超出这个窗口会让所有人的验证码"看着对但过不了"。**生产环境务必开启 NTP**:

```bash
timedatectl set-ntp true
timedatectl status   # 确认 "System clock synchronized: yes"
```

**恢复码**:每批 10 个,`XXXX-XXXX-XXXX` 形态,字符集同样剔除易混淆字符。**只在绑定完成的那一刻明文出现一次**,库里只存 SHA-256 摘要,任何后续请求(包括超管的)都取不回明文。登录时用掉即销毁(一次性);重新生成走 `POST /api/me/mfa/regenerate-recovery`,必须携带一个当前 TOTP 或一个未用过的恢复码作二次确认(否则被劫持的会话可以自行铸造长效凭据,还顺手作废真主人手里的码),旧批次整批作废。

**三层恢复通道**(从自助到运维,逐层升级):

| 层 | 手段 | 前提 | 入口 |
|---|---|---|---|
| ① 自助 | 用恢复码登录 | 手里还有未用过的恢复码 | 第二段登录框直接填恢复码 |
| ② 管理员 | 超管重置目标账号 MFA | 还有另一个能登录的超管 | `POST /api/admin/users/{id}/reset-mfa` |
| ③ 运维 | 本机 CLI 重置 | 能 SSH 到机器(root) | `proxyhubctl reset-mfa --username NAME` |

第③层是最终逃生门,与 SSH 隧道逃生门同一套哲学:**能进到这台机器的 shell 就已经是最高权限**,故不再做二次身份验证,只做防手误确认(交互式要求输入 `yes`,非交互调用需显式 `--yes`)。它只能被本机 shell 触达,永不经 HTTP 暴露。

```bash
# 交互确认后重置(账号回到"从未绑定"态,下次登录重走绑定流程)
proxyhubctl reset-mfa --username alice

# 非交互(脚本)调用
proxyhubctl reset-mfa --username alice --yes
```

重置的语义:丢弃 TOTP 密钥、`totp_enabled=0`、恢复码整批作废。会话**不**主动吊销(MFA 闸门每请求重读库,活跃会话立刻被重新关上,留着它反而省一次重新登录)。审计事件 `mfa_reset`。用户名不存在时非零退出并明确报错,不静默成功。

**重置不清受信 IP**:两者是独立动作。若怀疑设备或网络已失陷,重置之外还要清空受信列表(`POST /api/admin/users/{id}/trusted-ips/clear`,或用户设置页逐条撤销),否则该地址仍可免验直通。

### 登录加固:受信 IP

受信 IP 是某账号的**免 MFA 来源地址**列表,按用户隔离,初始为空。

**建立(只有显式决策,不静默)**:

- MFA 成功页勾选"信任此 IP 30 天";
- 采纳信任推荐:系统统计某地址在 30 天窗口内**真实通过第二因子**的登录次数,达 3 次即列为推荐(免验登录刻意记 `mfa_skipped=trusted_ip` 而非 `mfa=`,所以"免验"不会给自己刷推荐);
- 可选的**自动信任**模式(租户级设置 `auto_trust_ip`,**默认关闭**):开启后,达阈值的地址在 MFA 成功时自动入列。

**失效**:授权窗口 30 天;受信 IP 每次成功登录自动续期(为省写入,`last_used_at` 超过 24 小时才实际落库)。用户可在设置页"受信 IP"标签逐条撤销;超管可清空任意用户的整张列表。已过期的条目仍会列出并标记,便于用户看见并清理。

**loopback 不自动受信**:反向代理配置不当时每个请求看着都像 `127.0.0.1`,自动授权等于给共用这一跳的所有人发免验通行证。显式信任仍然允许。

**风险取舍**:受信 IP 把"某地址上的这个账号"降级为单因素(密码)。它适用于家庭/办公固定出口,不适用于共享出口(公司 NAT、蜂窝网关、公共 Wi-Fi)——同一出口后的其他人只要拿到密码就能进。

**审计**:`trusted_ip_added` / `trusted_ip_revoked` / `trusted_ip_auto_toggle` / `trusted_ip_cleared`;免验登录记 `login_success` 带 `mfa_skipped=trusted_ip` 标记。

### 内存态:重启即失效

三类登录态**只在进程内存中**,不落库,服务重启一律失效:

| 状态 | 生命周期 | 重启后表现 |
|---|---|---|
| 验证码挑战(challenge) | TTL 5 分钟 | 页面上的图作废,点刷新取新图即可 |
| 第二段握手(`mfa_pending`) | TTL 5 分钟 | 正在做第二步的用户回到密码页重登 |
| 登录会话(session) | 12 小时 | 所有人需重新登录 |

这是刻意的取舍:窗口都很短,丢失的代价是"重来一次",换来的是不必为短命凭据维护持久化与清理逻辑。反过来也是一条运维手段——重启服务即可掐断所有在飞行的登录中间态。

## 订阅地址防护(/sub)

订阅地址是全站唯一公开无鉴权入口,防护设计见 ADR 0033。核心规则:**所有防护判定都在 path + token 验证通过之后**——无效 token 永远只得统一 404(与地址不存在逐字节一致),429/403 只发给持有效 token 的请求。

### 拉取留痕

每次 /sub 请求(含被拦)都在 `pull_logs` 留一行,`status` 区分结果:成功 / 限频 / 地域拦截 / 地域观察 / 黑名单 / 已禁用 / 错误令牌。订阅详情抽屉的 IP 明细可见;**全局汇总与趋势只统计成功拉取**,扫描探测不会灌爆统计口径。

### 拉取限频

IP × 订阅地址滑动窗口,默认 60 次/小时(系统设置"订阅拉取限频阈值",0=关闭)。超限回 `429 + Retry-After`,规范客户端会自动退避。计数在内存,重启清零。

### 拉取黑名单

- **自动**:同一 IP 1 小时内被限频达 10 次("自动黑名单升级次数")→ 自动拉黑 24 小时("自动黑名单时长"),期间该 IP 拉任何订阅地址一律 403;
- **手动**:安全审计页"IP 规则"区块或订阅 IP 明细行内"封禁",可选时长或永久;
- 自动条目只作用于 /sub,超管确认后可**升级为整站拒止**。

### 地域白名单

订阅详情抽屉按地址配置,三档:**关闭**(默认)/ **观察**(判定不拦截,留"地域观察"记录)/ **拦截**(未命中 403)。建议先观察——确认 GeoIP 对自己设备的判定准确后再升拦截,蜂窝网出口常落在网关所在省。空列表 = 不限制;判定不到位置的来源在拦截档一律视为未命中(保守)。**省份配置当前不可用**:内嵌 GeoIP 库为 Country 级、无省级数据,省份项永不命中,请勿在拦截档使用(UI 已折叠警告)。

### 整站 IP 黑名单

安全审计页"IP 规则"区块新增 scope=整站拒止 的规则后,该来源访问**任何路径**(管理面、/sub、公开端点)一律 404——对它而言这站不存在。**loopback 永远豁免**:本机与 SSH 隧道(`ssh -L`)不受任何 IP 规则约束,这是误配时的逃生门。数据库异常时整站拒止 fail-open(不拦),不把纵深防御变成单点故障。

## 数据安全

### 敏感数据清单

ProxyHub 存储以下敏感数据:

| 数据类型 | 存储位置 | 加密状态 | 风险 |
|---------|---------|---------|------|
| 管理员密码 | `data.db` | bcrypt 哈希 | 低（需彩虹表 + 高算力） |
| **TOTP 密钥** | `data.db`（`users.totp_secret`） | **明文** | **高**（泄露可离线生成有效验证码，第二因子形同废除） |
| MFA 恢复码 | `data.db`（`users.recovery_codes_hash`） | SHA-256 摘要 | 低（不可逆，且单次使用即销毁） |
| 机场订阅 URL | `data.db` | 明文 | **高**（泄露可拉取节点） |
| 自建节点配置 | `data.db` | 明文 | **高**（含密码/UUID） |
| 订阅地址 Token | `data.db` | 明文 | 中（泄露可拉取聚合订阅） |
| Site Path | `data.db` + Caddy 配置 | 明文 | 低（知道路径仍需密码） |
| 备份归档 | `/var/lib/proxyhub/backups/` | **加密**（openssl enc） | 低（需 Site Path 派生密钥） |

> ⚠️ **data.db 现在含 TOTP 密钥,与节点凭证同一等级,必须按凭证等级对待。**
> 引入 MFA 后,数据库不只是"配置 + 哈希",它握着可直接复算第二因子的共享密钥:
> 拿到 `users.totp_secret` 的人可以离线生成该账号的有效验证码,MFA 对他不再是障碍。
> 因此对 `data.db` 的要求与对机场订阅 URL、自建节点密码/UUID 完全相同:
>
> - **文件权限**:安装器把 `/var/lib/proxyhub` 建为 `0750` 且属主 `proxyhub:proxyhub`(组外不可读)。别为了"方便看数据"放宽目录权限、别把 `data.db` 复制到家目录或 `/tmp`。收紧到仅属主可读更稳:
>   ```bash
>   chmod 0700 /var/lib/proxyhub
>   chmod 0600 /var/lib/proxyhub/data.db
>   ls -l /var/lib/proxyhub/data.db   # 应为 -rw------- proxyhub proxyhub
>   ```
> - **备份**:备份归档等价于一份 TOTP 密钥副本。异地存放要按凭证对待(二次加密、限制访问),不要丢进对象存储的公共桶,不要随手发进聊天工具。
> - **调试**:排障时 `sqlite3 data.db "SELECT * FROM users"` 会把密钥打到终端与 shell 历史里;需要看用户表时显式列出字段,跳过 `totp_secret` / `recovery_codes_hash`。
> - **失陷响应**:一旦怀疑 `data.db` 或某份备份泄露,重置全部账号 MFA(`proxyhubctl reset-mfa`)并清空受信 IP,与轮换密码/Site Path 同批处理——只改密码不换密钥,等于留着一把仍然有效的第二因子钥匙。

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
✅ **暴力破解**: 验证码软拦截 + IP2Ban 硬拦截 + 高强度密码  
✅ **凭证泄露**: TOTP 全员强制 —— 只拿到密码进不了管理面  
✅ **中间人攻击**: Caddy TLS + 出站 HTTPS 验证  
✅ **数据泄露**: 备份加密 + bcrypt 密码哈希  
✅ **配置错误**: 安装器自动化 + 健康检查验证  

### 非防护场景（Out of Scope）

❌ **主机沦陷**: 如果攻击者获得 root 权限，可读取所有数据（包括 `data.db` 里的 TOTP 密钥与解密备份），MFA 不再构成障碍  
❌ **共享出口下的受信 IP**: 把某地址设为受信,等于对该出口后的所有人只留密码一道门  
❌ **认证器设备失陷**: 手机被控可产生有效验证码;MFA 只防凭证泄露,不防设备沦陷  
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
- [ ] 首登已完成 MFA 绑定,10 个恢复码已存入密码管理器(离线抄一份更好)
- [ ] 系统时钟已开启 NTP（`timedatectl status` 显示 synchronized: yes）
- [ ] `data.db` 权限已收紧（`ls -l /var/lib/proxyhub/data.db`)
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
- [ ] 受信 IP 列表已复核（设置页"受信 IP",撤销已不再使用的地址）
- [ ] 恢复码余量充足（用掉过的话重新生成一批,旧批次整批作废）

### 安全事件响应

- [ ] 立即轮换 Site Path（`proxyhubctl rotate-path`）
- [ ] 轮换管理员密码
- [ ] **重置全部账号 MFA 并清空受信 IP**（`data.db` 或备份可能泄露 = TOTP 密钥可能泄露;只换密码等于留着一把仍然有效的第二因子钥匙）
- [ ] 重启服务（掐断所有在飞行的会话、验证码挑战与第二段握手 —— 三者都在内存）
- [ ] 检查审计日志（登录侧关注 `captcha_failure` / `mfa_failure` / `mfa_reset` / `trusted_ip_added` 是否有非本人操作)
- [ ] 检查封禁 IP（`SELECT * FROM banned_ips ORDER BY ban_until DESC;`）
- [ ] 恢复最近备份（验证数据完整性）
- [ ] 升级到最新版本（如有安全补丁）

## 合规性（可选）

ProxyHub 设计时未针对特定合规框架（GDPR、HIPAA、SOC 2），但提供以下安全控制:

- **访问控制**: 管理员认证 + 全员强制 MFA + 登录验证码 + IP2Ban
- **加密**: TLS 传输 + 备份加密
- **审计**: 登录安全事件流水（90 天:登录成功/失败、验证码失败、MFA 绑定/失败/重置、封禁、受信 IP 变更）
- **备份**: 自动化备份 + 恢复验证
- **密码管理**: bcrypt 哈希 + 高强度生成

如需满足特定合规要求，可能需要额外配置（如日志转发到 SIEM、定期渗透测试）。

## 参考资源

- [OWASP Top Ten](https://owasp.org/www-project-top-ten/)
- [NIST SP 800-63B: Digital Identity Guidelines](https://pages.nist.gov/800-63-3/sp800-63b.html)
- [Caddy Security Best Practices](https://caddyserver.com/docs/conventions#security)
- [mihomo Security](https://github.com/MetaCubeX/mihomo/security)

---

**最后更新**: 2026-07-20  
**版本**: 0.1.0-rc.1
