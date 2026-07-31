<p align="center">
  <img src="web/public/proxyhub-icon.png" alt="ProxyHub Logo" width="128" />
</p>

<h1 align="center">ProxyHub</h1>

<p align="center">
  把多个机场订阅聚合成一个统一订阅地址 —— 自动筛选最优节点，一个链接喂饱所有设备。
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="License: MIT" /></a>
  <a href="https://github.com/taliove/proxyhub/releases"><img src="https://img.shields.io/github/v/release/taliove/proxyhub?include_prereleases" alt="GitHub Release" /></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go" alt="Go Version" /></a>
  <a href="https://vuejs.org/"><img src="https://img.shields.io/badge/Vue-3.5-4FC08D?logo=vue.js" alt="Vue Version" /></a>
</p>

---

ProxyHub 是一个自托管的代理订阅聚合系统。它定时从多个上游机场拉取节点，经过健康检查、去重和地区筛选后聚合成节点池，再向你的每台设备暴露一个统一的订阅地址 —— 客户端只认这一个链接，节点优劣由 ProxyHub 替你操心。

```
机场订阅 A ┐
机场订阅 B ┼─> ProxyHub(定时拉取 -> 健康检查 -> 去重筛选)─> 一个订阅地址 ─> Clash / V2Ray 客户端
自建节点   ┘
```

## 核心特性

- 🔄 **订阅聚合** — 统一管理多个机场订阅，输出一个聚合订阅地址
- 🩺 **智能筛选** — 定时健康检查（延迟测速 + 真实请求）、自动去重、按地区精选、延迟排序
- 📡 **多格式输出** — 自动识别客户端，返回 Clash 或 V2Ray 格式
- 🖥️ **Web 管理界面** — 初始化向导、仪表盘、机场/节点/订阅管理，全程图形化操作
- 🛟 **自建节点兜底** — 自建节点作为 FailBack，与机场节点统一管理
- 👥 **多用户隔离** — 普通用户拥有独立的机场、节点池与订阅地址，超管统一分配配额
- 🔔 **告警通知** — 机场失效、节点不足时通过飞书 Webhook 及时通知
- 📦 **单二进制部署** — 内嵌前端、SQLite 与 mihomo 内核，零外部依赖

## 快速开始

### 1. 下载并解包

发布包命名格式为 `proxyhub_<版本>_<系统>_<架构>.tar.gz`，内含可执行文件与示例配置。以 Linux x86_64 为例（其他平台见 [Releases](https://github.com/taliove/proxyhub/releases)）：

```bash
# 解析最新版本号(经 releases API,稳定版/预发布都适用)
VERSION=$(curl -fsSL https://api.github.com/repos/taliove/proxyhub/releases | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p' | head -1)
curl -fsSLO "https://github.com/taliove/proxyhub/releases/download/v${VERSION}/proxyhub_${VERSION}_linux_amd64.tar.gz"
tar -xzf "proxyhub_${VERSION}_linux_amd64.tar.gz"
```

解包后可用 `./proxyhub version` 核对制品版本。

### 2. 启动服务

```bash
cp config.example.yaml config.yaml   # 默认配置即可使用,后续在 Web 界面配置
./proxyhub
```

### 3. 打开浏览器完成初始化

访问 `http://localhost:8080`，按向导完成：

1. 设置管理员账户
2. 配置安全策略(IP2Ban)
3. 登录系统

### 4. 添加机场并创建订阅

1. 进入"机场管理" → 添加机场订阅 URL
2. 进入"订阅地址" → 创建订阅地址
3. 复制订阅 URL 到你的代理客户端

完成！🎉

## 生产部署

一键安装器自动配置 systemd 服务、Caddy HTTPS 反向代理和运维工具：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh)
```

国内用户：GitHub 并非一律不可达，**先试上面的默认命令**；有本地代理就加 `https_proxy` 前缀（安装器内部下载同样走代理）。仅当 GitHub 完全不可达时，才走 jsDelivr 入口 + 镜像下载基：

```bash
bash <(curl -fsSL https://cdn.jsdelivr.net/gh/taliove/proxyhub@main/install.sh) \
  --non-interactive --domain <你的域名> --version <版本> \
  --download-base https://<镜像>/taliove/proxyhub/releases/download
```

制品经 minisign 签名，镜像下载同样可验证真伪。详见 [国内部署](docs/DEPLOY.md#国内部署网络受限环境)。

安装后使用 `proxyhubctl` 运维：`status` / `logs` / `backup` / `update` / `rotate-path`。

详细安装、备份、更新、卸载流程见 **[生产部署指南](docs/DEPLOY.md)**。

### Docker(开发/测试环境)

镜像发布在 GitHub Container Registry(GHCR),`:latest` 跟踪最新稳定版：

```bash
docker run -d \
  -p 127.0.0.1:8080:8080 \
  -v ./data:/data \
  --name proxyhub \
  ghcr.io/taliove/proxyhub:latest
```

> 端口必须绑回环(`127.0.0.1:8080:8080`):新装实例的 `/api/setup` 未鉴权，
> 直接暴露到网络等于把管理员账号交给最先到达的人。确需远程初始化时，
> 设置 `PROXYHUB_SETUP_TOKEN` 并在初始化请求头携带 `X-Setup-Token`。
> Docker 部署仅适合本地开发。生产环境请使用一键安装器（自动配置 HTTPS）。

## 常见问题

- **支持哪些代理协议？** VMess、VLess、Trojan、Shadowsocks(内嵌 mihomo 内核)。
- **订阅拉取失败 "no available nodes"？** 等待首次健康检查完成（启动后约 15 分钟）。
- **忘记管理员密码？** 可通过备份恢复或手动重置数据库。
- **如何添加自建节点？** 管理后台「系统设置」中配置。

更多问题见 **[FAQ](docs/FAQ.md)**。

## 安全

ProxyHub 采用纵深防御：仅监听环回地址、Caddy 强制 HTTPS、管理后台随机路径、登录失败自动封禁、备份加密。

威胁模型与防护措施详见 **[安全模型](docs/SECURITY.md)**。

## 文档

**使用者**

- [生产部署指南](docs/DEPLOY.md) — 安装、备份、更新、卸载
- [常见问题](docs/FAQ.md) — 使用与运维问答
- [安全模型](docs/SECURITY.md) — 威胁模型与防护措施

**开发者**

- [开发指南](docs/DEVELOPMENT.md) — 环境搭建与开发循环

## 贡献

欢迎提交 Issue 和 Pull Request!

## 许可证

MIT License — 详见 [LICENSE](LICENSE)

本产品内置并使用了 [DB-IP](https://db-ip.com) 的 IP 地理位置数据（DB-IP Lite，依据 CC BY 4.0 许可）。
This product uses DB-IP data, CC BY 4.0, https://db-ip.com

---

<p align="center">
  <a href="https://github.com/taliove/proxyhub">github.com/taliove/proxyhub</a>
</p>
