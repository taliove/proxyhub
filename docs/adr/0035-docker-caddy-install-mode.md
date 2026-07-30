# ADR 0035: 安装器 Docker Caddy 模式

## 状态

accepted

## 上下文

一键安装器(`install.sh` + `scripts/install/lib.sh`)此前只认一种 Caddy 拓扑:宿主机原生 systemd Caddy(`command -v caddy` 探测、`/etc/caddy/Caddyfile` + `import conf.d/*.caddy`、`caddy fmt/validate`、`systemctl reload`)。但相当比例的既有部署把 Caddy 跑在 Docker 容器里(`docker run -p 80:80 -p 443:443 -v ...:/etc/caddy caddy`),这类目标机上安装器直接 fail closed("Caddy v2 is required but not installed" 或 80/443 被 docker-proxy 占用),用户只剩 `--no-caddy` 手工合并一条路。

关键权衡:

1. **支持形态**:全容器化编排(连 ProxyHub 本体也容器跑)是另一种产品形态,与 systemd 安装器平行、工作量与文档面都翻倍;只放行不集成则与 `--no-caddy` 差异过小。真正缺的一环是"识别并集成目标机上既有的 Docker Caddy 容器",ProxyHub 本体维持 systemd 裸机。
2. **容器选择**:目标机可能跑多个 caddy 容器,纯自动探测可能选错;纯显式旗标则跟"恰好一个容器"的最常见情形过不去。
3. **配置投递**:`docker cp` 进容器层会在容器重建时丢失,与"受管安装"的持久性承诺冲突;必须落在容器 `/etc/caddy` 的持久挂载上。
4. **回连拓扑(最硬的一条)**:宿主回环对容器网络命名空间不可路由,桥接容器永远够不到 `127.0.0.1:8080`;而宪法红线是管理面只走 Site Path + loopback(AGENTS.md §7)。桥接部署要连通,管理面就必须绑定到网桥网关地址,信任边界从 loopback 扩到该 docker 网桥。
5. **XFF 信任随之变化**:原生拓扑下 Caddy 与 ProxyHub 的对端是 loopback,XFF 采信缺省即安全;桥接拓扑下对端是容器 IP,且同网桥任意容器都能伪造 XFF 直连管理面。代码已预留 `server.trusted_proxies`(CIDR 列表,缺省 loopback 惯例)这道闸门。

## 决策

1. **Caddy 模式三态**:`native`(宿主机 systemd Caddy,现状)/ `docker`(容器 Caddy,本 ADR 新增)/ `none`(`--no-caddy`,现状)。模式记录在安装档案 `CADDY_MODE` 字段。
2. **容器选择:混合制**。`--caddy-docker <容器名>` 显式指定(校验存在、运行中、caddy 镜像);不指定时恰好一个运行中的 caddy 镜像容器→自动选用并明示,零个/多个→fail closed。模式优先级:`--caddy-docker` 强制 > 有 caddy 二进制走 native > 自动探测单个 docker caddy 容器 > 维持报错。
3. **配置投递:解析持久挂载**。`docker inspect` 解析容器 `/etc/caddy` 挂载——bind mount 取 Source 路径,named volume 解析 `/var/lib/docker/volumes/.../_data`;fragment 经宿主机路径写入、Caddyfile 追加 `import /etc/caddy/conf.d/*.caddy`(容器路径语义)。单文件挂载等形态 fail closed 并打印修复指引。docker 模式下校验容器已发布 80/443。
4. **回连拓扑:自动识别双路径**。容器为 `network_mode: host`→零改动(loopback 原样,fragment 不变);桥接网络→`config.yaml` 的 `server.host` 写该容器所在网桥的网关 IP,`trusted_proxies` 收窄到该网桥子网,fragment 用 `reverse_proxy host.docker.internal:PORT`,并校验容器具备 `host.docker.internal:host-gateway` 映射(缺失→fail closed + `--add-host` 修复指引)。
5. **Caddy 操作镜像 native 语义**:`docker exec <容器> caddy fmt/validate/reload`;admin API 被禁用(如 `admin off`)时 fallback `docker restart <容器>` 并警告对其他站点的短暂中断(与 native 的 `systemctl restart` fallback 同构)。写 fragment → fmt → validate → reload 任一环失败即回滚(删 fragment、还原 Caddyfile 备份),与 native 一致。
6. **proxyhubctl 全量对齐**:安装档案新增 `CADDY_MODE` 与 `CADDY_CONTAINER`;update/backup/restore/rotate-path/uninstall 读档案后自动走 docker 通道,容器失联时 fail closed。
7. **薄集成测试**:新增 `scripts/install/test_docker_caddy.sh`——真 `caddy:2` 容器 + bind mount 配置目录到 scratch 树,安装器以 `PROXYHUB_ROOT` 测试模式运行,docker seam 打真、systemctl/curl 照旧 mock;覆盖容器探测、挂载解析、fragment 投递、fmt/validate/reload、坏配置回滚、零容器/多容器/无挂载 fail closed、uninstall 清理;无 docker 环境自动 skip。

## 理由

- 识别并集成(而非全容器化或仅放行)填补了真实缺口:既有 docker caddy 部署的用户此前只能走 `--no-caddy` 手工合并,而安装器的价值恰恰在受管集成(写配置、校验、回滚、健康验证、运维对齐)。
- 混合制容器选择与安装器的 fail-closed 性格一致:常见情形零输入,歧义情形拒绝猜测。
- 挂载解析把配置投递收敛为与 native 同构的"写文件 + import 行"机制,fragment 与 Caddyfile 的生命周期(备份、回滚、uninstall 清理)整套复用;`docker cp` 路径因容器重建即丢被否决。
- 桥接拓扑绑定网关 IP 是对红线的**有界**放宽:管理面从"仅本机 loopback"扩到"仅该 docker 网桥",Site Path 保密、XFF 替换、`trusted_proxies` 子网收窄三道闸门仍在;host 网络容器则一寸不让。拒绝桥接(仅认 host 网络)会把最常见的 compose 桥接用户整体拒之门外,功能名存实亡。
- 全量对齐 proxyhubctl 是"受管安装"承诺的一部分:装得上却维护不了(rotate-path 是常用操作)等于半成品。

## 后果

- **安全降级有界且明示**:桥接模式下同网桥任意容器可直连管理面(无 TLS)且可伪造 XFF 绕过 IP 层防御;该取舍写进安装摘要与 DEPLOY.md,建议运维将 caddy 隔离在独立网桥。对 docker 网桥内其他容器不信任的部署应使用 host 网络容器或 native Caddy。
- `--listen-addr` 校验与 `config.yaml` 的 `server.host` 写法在 docker 桥接模式下改为网关 IP(不再恒为 `127.0.0.1`);loopback 健康检查随之打该地址。
- 安装档案格式新增两个字段;旧档案(native)缺省按 `CADDY_MODE=native` 兼容。
- 术语见 CONTEXT.md「Caddy 模式」「Docker Caddy 模式」。
