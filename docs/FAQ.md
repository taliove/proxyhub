# 常见问题(FAQ)

面向 ProxyHub 使用者的常见问题与解答。部署运维细节见 [DEPLOY.md](DEPLOY.md)，安全模型见 [SECURITY.md](SECURITY.md)。

## 安装与账户

### Q: 忘记/重置管理员密码怎么办？

A: 安装器生成的账户信息保存在 `/root/.proxyhub-install-info`(仅包含账户名，不含密码)。如果密码丢失：

```bash
# 方案1: 恢复备份
proxyhubctl restore /var/lib/proxyhub/backups/latest-backup.tar.gz.enc --yes

# 方案2: 手动重置(需停止服务并修改数据库)
systemctl stop proxyhub
sqlite3 /var/lib/proxyhub/data.db "UPDATE settings SET value='<new-bcrypt-hash>' WHERE key='admin_password';"
systemctl start proxyhub
```

开发环境可直接删除 `var/data/data.db` 重新初始化。

### Q: 手机丢了 / 认证器被删，登录不进去怎么办？

A: 三条恢复通道，从自助往上升级，取第一条你还够得着的：

**① 用恢复码（手里还有未用过的码）**

第二段登录框直接填恢复码，不用填 6 位验证码——后端先按 TOTP 试，不匹配再按恢复码试。**一个码只能用一次**，进去之后立刻重新生成一批（见下面"恢复码丢了"一问，旧批次整批作废）。

**② 让另一个超管重置（团队里还有能登录的超管）**

超管调 `POST /api/admin/users/{id}/reset-mfa`(当前无界面按钮，走 API):

```bash
curl -X POST https://proxy.example.com/<site-path>/api/admin/users/42/reset-mfa \
  -H "Cookie: session=<超管会话>"
```

**③ SSH 到机器上重置（前两条都没了）**

```bash
proxyhubctl reset-mfa --username alice        # 交互确认,需输入 yes
proxyhubctl reset-mfa --username alice --yes  # 脚本调用
```

三种方式的结果一样：账号回到"从未绑定"态（密钥丢弃、恢复码整批作废），下次登录用密码进来后被引导重走绑定流程。会话不会被踢掉。

**别忘了受信 IP**:重置 MFA 不动受信列表。如果怀疑设备/网络已失陷，还要清空受信 IP，否则那些地址仍然免验直通：

```bash
curl -X POST https://proxy.example.com/<site-path>/api/admin/users/42/trusted-ips/clear \
  -H "Cookie: session=<超管会话>"
```

### Q: 验证码一直提示错误 / "invalid verification code"?

A: 按可能性从高到低排查：

1. **服务器时钟漂移**——最常见。TOTP 步长 30 秒，只容忍前后各一步（约 ±30 秒）。超出这个窗口，手机上的码看着对但一律过不了：

   ```bash
   timedatectl status          # 看 "System clock synchronized"
   timedatectl set-ntp true    # 没开就开
   ```

2. **手机时钟漂移**——同理，把手机设成自动同步时间。
3. **码已经过期**——输入过程中跨过了时间窗，等下一个码再试。
4. **扫了旧二维码**——绑定页每次打开都会重新签发一个待确认密钥，旧二维码随之作废。刷新页面重新扫。
5. **认证器里有多个同名条目**——重置重绑后，旧条目还在 App 里，很容易挑错。删掉旧的。

### Q: 恢复码丢了，能再看一次吗？

A: 不能。恢复码只在绑定完成的那一刻明文出现一次，库里只留 SHA-256 摘要，任何接口（包括超管的）都取不回明文。

但还能登录的话可以**换一批**：`POST /api/me/mfa/regenerate-recovery`，请求体带一个当前 TOTP 或一个未用过的恢复码作二次确认（当前无界面入口，走 API）：

```bash
curl -X POST https://proxy.example.com/<site-path>/api/me/mfa/regenerate-recovery \
  -H "Cookie: session=<你的会话>" -H "Content-Type: application/json" \
  -d '{"code":"123456"}'
```

旧批次整批作废——"码泄露了"也用这个接口善后。已经登不进去了的话，走上面的重置流程。

### Q: 登录页一直要我输图形验证码，怎么关？

A: 它是按 IP 的失败次数自动上的：同一 IP 的历史登录失败次数达到 `captcha_trigger_threshold`(默认 1)就要求验证码。成功登录一次会清零该 IP 的失败计数，验证码随之消失。

想调阈值（超管专属键，当前不在设置页表单里）：

```bash
# 改成失败 3 次才上验证码
curl -X PUT https://proxy.example.com/<site-path>/api/settings \
  -H "Cookie: session=<超管会话>" -H "Content-Type: application/json" \
  -d '{"settings":{"captcha_trigger_threshold":"3"}}'
```

`0` 表示常驻要求验证码。**不建议设成很大的值**——它是爆破的第一道减速带，而且比封禁温和（不误伤真人）。来源为 `127.0.0.1` 时本来就豁免。

### Q: 为什么这次登录没要 MFA，上次要了？

A: 那个地址在你的受信列表里。受信 IP = 该账号在该来源地址上免第二因子，窗口 30 天，每次登录自动续期。

只有显式动作会建立受信：MFA 成功页勾"信任此 IP"、采纳信任推荐（该地址 30 天内真实过了 3 次第二因子）、或者开了自动信任（`auto_trust_ip`，默认关）。审计里这类登录记 `login_success` 且带 `mfa_skipped=trusted_ip` 标记，与真过了 MFA 的 `mfa=totp` 区分得开。

想改：设置页 → "受信 IP"，可看列表（含已过期条目）、逐条撤销、开关自动信任。**公司 NAT、蜂窝网关、公共 Wi-Fi 这类共享出口别设受信**——同一出口后的其他人只要有你的密码就能进。

### Q: 重启服务后大家都被踢下线了？

A: 预期行为。会话（12 小时）、图形验证码挑战（5 分钟）、第二段登录握手（5 分钟）三者都只存在进程内存里，不落库，重启一律失效。窗口都很短，代价是"重来一次"。

反过来这也是一条应急手段：怀疑有会话或登录握手被劫持时，`systemctl restart proxyhub` 就能一次性掐断全部在飞行的状态。

### Q: 新建的用户登录后什么都点不动，一直跳绑定页？

A: MFA 对所有账号强制，没有豁免角色。未绑定认证器的会话在业务接口上一律 403 + `must_enroll_mfa`，前端据此跳绑定页。只有绑定页自己需要的四条路径放行：读自己信息、改自己密码、绑定接口、登出。

完成绑定（扫码 + 提交一次验证码 + 保存恢复码）后，**同一个会话**立刻放行，不用重新登录。

### Q: 如何更换域名？

A: 需要更新 Caddy 配置和 DNS:

```bash
# 1. 编辑 Caddy 配置
vim /etc/caddy/conf.d/proxyhub.caddy
# 修改 proxy.example.com 为新域名

# 2. 重载 Caddy
systemctl reload caddy

# 3. 更新 DNS A/AAAA 记录指向服务器 IP
```

Docker Caddy 模式的实例：配置片段在容器 `/etc/caddy` 挂载的宿主机路径下，重载用 `docker exec <容器名> caddy reload --config /etc/caddy/Caddyfile`。

### Q: 端口冲突怎么办？

A: ProxyHub 监听 `127.0.0.1:8080`(环回地址)。如果冲突，编辑 `/etc/proxyhub/config.yaml`:

```yaml
server:
  listen: "127.0.0.1:8081"  # 修改端口
```

同时更新 Caddy 配置 `/etc/caddy/conf.d/proxyhub.caddy` 中的 `reverse_proxy` 目标端口。

### Q: 服务器上跑着多个 caddy 容器，安装器会选哪个？

A: 一个都不选——多个候选属于歧义情形，安装器 fail closed，列出候选容器名并要求显式指名：

```bash
bash install.sh --non-interactive --domain proxy.example.com \
  --caddy-docker my-caddy
```

恰好只有一个运行中的 caddy 镜像容器时，安装器才会自动选用（安装日志会明示选的是哪个）；给了 `--caddy-docker` 则以你指定的为准。注意 `--caddy-docker` 与 `--no-caddy` 互斥，同给即报错退出。

### Q: 为什么只挂载 Caddyfile 单文件的 caddy 容器被安装器拒绝？

A: 安装器要把配置片段写进 `/etc/caddy/conf.d/` 并在 Caddyfile 里追加 import 行，这要求 `/etc/caddy` 整个目录是持久挂载（bind mount 或 named volume）。只挂载单个 Caddyfile 时，写进容器层的 conf.d 会在容器重建时丢失，与"受管安装"的持久性承诺冲突，所以 fail closed。改造方法（以 bind mount 为例）：

```bash
mkdir -p /srv/caddy
docker cp <容器名>:/etc/caddy/. /srv/caddy/   # 搬出现有配置
# 重建容器:把单文件挂载换成目录挂载
docker run -d --name caddy \
  -p 80:80 -p 443:443 \
  -v /srv/caddy:/etc/caddy \
  --add-host host.docker.internal:host-gateway \
  caddy:2
```

### Q: caddy 容器重建后，ProxyHub 的反代配置还在吗？

A: 在——只要重建后的容器仍然挂载同一个 `/etc/caddy` 持久目录（bind mount 或 named volume），配置片段和 Caddyfile 的 import 行都落在挂载上，容器层丢了不影响。但两点要注意：

1. **容器名变了**：安装档案（`/root/.proxyhub-install-info`）里的 `CADDY_CONTAINER` 记的是安装时的容器名，改名后 `proxyhubctl` 会因找不到容器而 fail closed。编辑档案把 `CADDY_CONTAINER` 改成新容器名即可，不必重装；
2. **桥接拓扑参数丢了**：重建时丢了 `--add-host host.docker.internal:host-gateway`、80/443 端口发布或换了网桥，`proxyhubctl rotate-path` 等操作会在校验阶段 fail closed，按报错指引补齐参数即可。

## 国内网络环境

### Q: 国内服务器（GitHub 不可达）怎么装 ProxyHub？

A: 三步：入口脚本走 jsDelivr CDN，制品下载用 `--download-base` 指向一个你可达的镜像，并且**必须显式 `--version`**（镜像模式下 latest 自动解析不可用，安装器会直接拒绝）：

```bash
curl -fsSL https://cdn.jsdelivr.net/gh/taliove/proxyhub@main/install.sh -o install.sh
bash install.sh --non-interactive --domain proxy.example.com \
  --version <已发布版本号> \
  --download-base https://<镜像>/taliove/proxyhub/releases/download
```

镜像怎么选、可达性怎么自查、caddy 容器镜像与 GeoIP 的配套说明，见 [DEPLOY.md](DEPLOY.md)「国内部署」一节。

### Q: 从第三方镜像下载的制品安全吗？

A: 安全性不靠镜像自觉，靠密码学验签。发布方用私钥给校验和文件（SHA256SUMS）签了名，release 资产里多一个 `SHA256SUMS.minisig`；安装器与更新器内嵌对应公钥，下载后先验签、再按验过的校验和核校 tarball，任何一步不过都拒绝安装。恶意镜像可以同时替换 tarball 和校验和，但伪造不出能通过内嵌公钥的签名——缺签名文件、签名对不上，安装直接中止（fail closed）。验签只用系统自带的 openssl，不装新工具。原理与决策见 [ADR 0036](adr/0036-artifact-signing-and-mirror-channel.md)。

### Q: 国内装的实例，`proxyhubctl update` 怎么用？

A: 直接裸跑即可：

```bash
proxyhubctl update
```

安装时的下载基记在 `/root/.proxyhub-install-info` 的 `DOWNLOAD_BASE` 字段，update 自动沿用——当初走镜像装的，升级时不必再传 `--download-base`；latest 解析在 GitHub 不通时自动回退 jsDelivr 数据 API（与安装器同一条链）。两路都不通时会报错并提示，此时显式给版本：`proxyhubctl update --version <目标版本号>`。要换镜像时显式给 `--download-base`，显式参数优先于档案里的值。

## 订阅与节点

### Q: 订阅突然拉不到了(403/404/429)，怎么排查？

A: 打开订阅详情抽屉的「拉取统计」，看该 IP 最近记录的**状态**列：

- **错误令牌/已禁用**：URL 错了或地址被禁用——检查链接与开关；
- **限频**：触发限频（默认 60 次/小时），客户端稍后自动恢复；误伤可调「系统设置 → 订阅拉取限频阈值」；
- **黑名单**：被拉黑了（自动升级或手动），到「安全审计 → IP 规则」找对应条目删除或等过期；
- **地域拦截/地域观察**：该地址开了地域白名单——在抽屉「地域白名单」段检查配置，建议先切回「观察」档确认 GeoIP 判定；
- **什么都 404**:可能命中了整站拒止规则，同去「IP 规则」排查；本机 `ssh -L 8080:127.0.0.1:8080` 隧道永远不受规则约束，是排查逃生门。

### Q: 支持哪些代理协议？

A: VMess、VLess、Trojan、Shadowsocks(通过内嵌 mihomo 内核支持)。

### Q: 如何添加自建节点？

A: 在管理后台"系统设置"中配置自建节点信息。自建节点可作为 FailBack，与机场节点统一管理。

### Q: 订阅地址拉取失败 "no available nodes"?

A: 等待首次健康检查完成（启动后 15 分钟内）。查看日志：

```bash
proxyhubctl logs --lines 100 | grep health
```

## 代理内核

### Q: 内嵌的代理内核如何更新？

A: mihomo 内核内嵌在 ProxyHub 二进制中。运行 `proxyhubctl update` 会同时更新 ProxyHub 与内核，无需单独更新。

### Q: 如何调整健康检查行为？

A: 内核配置由 ProxyHub 内部生成，无需也不应手动修改。如需调整健康检查行为，编辑 `/etc/proxyhub/config.yaml` 中的 `health_check` 部分。

## 获取帮助

遇到未覆盖的问题，请提供：
1. 完整日志(`proxyhubctl logs --lines 500`)
2. 系统信息(`proxyhubctl show-info`)
3. 错误截图或描述

GitHub Issues: https://github.com/taliove/proxyhub/issues
