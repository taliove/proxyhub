---
name: ops
description: ProxyHub 运维领域——服务运行生命周期(start/stop/restart/status/日志验证)、发布管理(版本纪律、发布前演练、打 tag、GitHub Actions 发布、发布后验证与回滚)与生产部署(install.sh 预检/执行/验证/分诊,含国内网络与 docker caddy 集成)
---

# 运维(ops):运行生命周期 + 发布管理 + 生产部署

## Part 1 — 运行生命周期

运行生命周期只有 make 一个入口(`start.sh` 已退役删除,勿再引用):

```bash
make restart        # 重启服务(停旧启新,幂等;日志 var/log/proxyhub.log,pid 同目录)
make start          # 后台启动(已在运行会提示,不会起第二个实例)
make stop           # 停止
make status         # 状态
tail -f var/log/proxyhub.log
make dev-backend    # 后端开发(config.example.yaml,storage 指向 var/data/data.db)
```

### 重启验证(代替原 /restart 命令)

依次执行 `make build`、`make restart`、`make status`,然后:

1. 报告服务状态与 `var/log/` 最新日志尾部,确认无启动错误
2. `curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/` 返回 200
3. 若本次改动涉及前端,明确说明已走完整 `make build`(go:embed),禁止只 `make build-backend`
4. 不要只说"已重启"——给出可核对的证据(进程状态/日志行)

## Part 2 — 发布管理(原 release)

发布由 GitHub Actions 自动完成(`.github/workflows/release.yml`,tag `v*` 触发),本部分管理"人"的这一侧:版本、演练、验证。pipeline 内部的事(构建矩阵、打包、attest、GHCR)不要手工干预。

### 0. 版本纪律(不可违反)

- `VERSION` 文件是版本的**唯一事实源**;release.yml 会拒绝与 `v$(cat VERSION)` 不一致的 tag
- 严格 SemVer:`MAJOR.MINOR.PATCH`,预发布用 `-rc.N` 后缀;**禁止 `+` 构建元数据**(GHCR tag 不支持)
- 预发布版本(含 `-`)会自动标记为 GitHub prerelease;`proxyhubctl update --stable-only`(自动更新 timer)会跳过它
- 制品命名契约:`proxyhub_<version>_<os>_<arch>.tar.gz` + `SHA256SUMS`,下划线分隔——`install.sh` 和 `proxyhubctl update` 都按此消费,改命名必须三处同步(release/package.sh、install.sh、proxyhubctl)

### 1. 发布前演练(全本地,必做)

```bash
make check        # vet + Go 测试 + shell 套件,全绿
make build        # 完整构建可用

# 打包演练(单目标提速;完整矩阵由 CI 跑)
TARGETS="linux/amd64" bash scripts/release/package.sh
bash scripts/release/verify.sh dist/release

# 远程真机一键安装演练(需 scripts/release/rehearsal.local.conf,样例见同目录 example)
bash scripts/release/rehearse_install.sh
```

- 检查 `dist/release/` 制品名是否符合命名契约(下划线!)
- 确认 `VERSION` 已更新,且与想打的 tag 一致
- 过一遍自上次发布以来的 `git log --oneline`,确认没有半成品提交混进 main
- 远程演练在真机上完整走一遍一键安装(随机域名、跳过公网证书健康检查)并无痕卸载、零残留校验;目标机 SSH 信息只放 gitignored 的 `rehearsal.local.conf`,**永不签入**。演练失败的清理同样自动执行(trap 兜底),但失败后要人工复核残留。

### 2. 发布(remote 建好之后)

```bash
git tag -a v$(cat VERSION) -m "release v$(cat VERSION)"
git push origin main --tags
```

之后只做观察,不做补救式手改:

```bash
gh run watch                         # 盯 validate -> test -> package -> docker
gh release view v$(cat VERSION)      # 确认制品、SHA256SUMS、prerelease 标记
```

pipeline 阶段:validate(tag==VERSION、SemVer)→ test(make vet/test)→ package(矩阵打包 + checksum 验证 + attest + 上传)→ docker(GHCR 多架构镜像 + attest)。任一阶段失败,看日志修代码,**改完发新 tag,不删已推的 tag**。

### 3. 发布后验证(必做)

```bash
# 下载一个制品 + SHA256SUMS,用仓库自带工具校验
gh release download v$(cat VERSION) -p "proxyhub_$(cat VERSION)_linux_amd64.tar.gz" -p SHA256SUMS -D /tmp/ph-verify
bash scripts/release/verify.sh /tmp/ph-verify
```

- [ ] 制品数量与矩阵一致(linux amd64/arm64 + darwin + windows)
- [ ] attest provenance 存在(release 页面 / `gh attestation verify`)
- [ ] GHCR 镜像 `ghcr.io/<owner>/proxyhub:<tag>` 可拉取;稳定版还有 `:latest`
- [ ] 干净环境冒烟(可选但推荐):Linux 容器/VM 里跑 `install.sh --non-interactive`,或至少 `bash scripts/install/test_install.sh`

### 4. 回滚原则

- **已发布的 tag 和制品不可变**:发现缺陷,修复后发 `PATCH`/`rc.N+1` 新版,不删旧 tag(下游可能已 pin checksum)
- 极端情况(制品含秘密):`gh release delete`,但视为已泄露走轮换流程,而不是假装没发生

### 5. 首次推送前(一次性)

走 `pre-push` skill 全项,额外确认:仓库可见性(private/public)、LICENSE 文件、`secrets.GITHUB_TOKEN` 权限(contents: write / packages: write / attestations 由 workflow 声明,无需手动配)。

## Part 3 — 生产部署(install.sh)

用户向步骤归 `docs/DEPLOY.md`(链接入口、镜像契约);本部分是**代用户执行部署**的操作规程,全部条目都在真机(国内网络 + docker caddy 自建镜像,2026-08)验证过。

### 1. 预检清单(按序,任何一项不满足先解决再装)

1. SSH 免密 + `sudo -n true` 免密;Ubuntu 22.04/24.04、systemd、无既有安装(`ls /usr/local/bin/proxyhub` + `sudo ls /root/.proxyhub-install-info`)。
2. **80/443 占用者定性**:`sudo ss -tlnp | grep -E ':(80|443) '`——原生 caddy / docker caddy 容器 / 他物(他物占用即冲突,先清)。
3. **DNS 与入站**:域名解析结果 == 本机公网 IP(`curl ifconfig.me`);**云安全组/防火墙必须放行 443(DNS-01 场景)或 80+443(HTTP-01 场景)的入站**——出站正常不代表入站通,这是生产部署最常见的失败点(LE 报 "Timeout during connect (likely firewall problem)")。预检时从一台外部机器实测 `curl --max-time 8 https://<domain>/`。
4. **出站探测**:`curl --max-time 10 -o /dev/null https://github.com`(决定制品走官方源还是自动回退 gh-proxy.com)、`https://data.jsdelivr.com`(latest 解析回退通道)。
5. **已有 caddy 摸底**:容器镜像名(官方 `caddy:*` 还是自建如 `caddy-dnspod:*`)、网络模式(host/bridge)、配置布局(单文件 Caddyfile 挂载 / conf.d 目录)、`admin` 是否 off(reload 会降级为容器重启,其他站点瞬断,先告知用户)、**当前 ACME CA 是生产还是 staging**(自定义镜像可能把默认 patch 成 staging——staging 证书不被公开信任,签下来也是废的;查 `docker logs` 里 `acme_client` 的 `"ca"` 字段)。

### 2. 国内网络三定律

1. 入口脚本走 jsDelivr;制品下载 github 不可达自动回退 gh-proxy.com,**可达但限速**(探针过、大文件 stall)也会失败后自动回退——都不需要手动 `--download-base`。
2. **生产一律显式 `--version`**:latest 解析依赖 GitHub 重定向 + jsDelivr 数据 API 双通道,国内都不稳;显式版本同时换来可复现部署。
3. jsDelivr `@main` 有缓存(最长约 12h):**刚推送的安装器修复不会立刻生效**。精确部署用 `@<commit-sha>` 入口,或 scp `install.sh` + `scripts/install/{lib.sh,proxyhubctl}` 到目标机按目录布局跑(SCRIPT_DIR 相邻即免下载)。

### 3. Docker caddy 集成要点

- 自动检测只认官方镜像名;自建插件镜像(caddy-dnspod 等)必须显式 `--caddy-docker NAME`(容器内 `caddy version` 功能探针兜底)。
- host networking 免除 80/443 发布检查且管理面拓扑不变;bridge 需发布 80/443 + host-gateway 映射,且管理面信任域扩到网桥(摘要会警告)。
- **DNS-01 场景的安全组只需 443**:`acme_dns dnspod` 写在**全局块**而非托管站点块(托管块会被安装器/rotate-path 重写);v0.0.4 的 dnspod provider 只收**单参** `ID,Token`(老式 LoginToken),用 `{env.X},{env.Y}` 从容器环境变量拼;改完全局块必须 `caddy validate` + 重启容器,语法错误会让容器 crash-loop 拖死所有站点——先 `cp` 备份,出错秒级恢复。
- staging CA 修复同样写全局块:`acme_ca https://acme-v02.api.letsencrypt.org/directory`。

### 4. 执行与验证

```bash
sudo bash -c "umask 077; nohup bash install.sh --non-interactive \
  --domain <domain> --version <x.y.z> [--caddy-docker <name>] \
  >/root/proxyhub-install.log 2>&1 &"
```

- 日志含一次性凭证,必须 root-only;监控用轮询日志 + `kill -0 <pid>`。
- 失败分层读日志:下载层(falling back / download failed)→ 校验层(minisign/checksum)→ 服务层(journalctl -u proxyhub)→ 边缘层(公网健康检查 = DNS/安全组/ACME,安装器本体此时已全部就绪)。
- 部署后验证:`systemctl is-active proxyhub`、`proxyhubctl status`、环回 healthz、**从外部机器**验 `https://<domain>/<site-path>/healthz`(本机自测公网域名可能因 hairpin NAT 误报)、证书 issuer 是生产 LE。

### 5. 两条铁律

1. **凭证只在完全成功的一次性摘要里出现**(管理员密码不落日志、不落安装记录)。安装中途失败 = 密码未交付,不要翻日志找(找不到);修复卡点后用 `proxyhubctl uninstall` 完整卸载再重装,走正规流程拿凭证。
2. **安装记录写在健康检查之后**:健康检查失败的安装没有 `/root/.proxyhub-install-info`,proxyhubctl 不认识这次安装——重装前必须 uninstall 清干净(service、配置、caddy 托管块)。

## 完成标准

运行:状态可查 + 日志无新增 ERROR + 接口实测 200。
发布:VERSION/tag 一致 + 演练绿 + pipeline 绿 + 制品校验通过。任何一步存疑,停下来,不发。
