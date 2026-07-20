# ProxyHub

现代化的代理订阅聚合系统 - Vue 3 + Go 单二进制部署

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![Vue Version](https://img.shields.io/badge/Vue-3.5-4FC08D?logo=vue.js)](https://vuejs.org/)

## ✨ 核心特性

### 🎨 现代化 Web 界面
- **Vue 3 + TypeScript + Element Plus** - 专业的现代化界面
- **初始化向导** - 首次使用图形化配置，无需手动编辑配置文件
- **实时管理** - 所有配置在 Web 界面完成，实时生效无需重启
- **仪表盘** - 统计概览、节点状态、拉取记录一目了然

### 🔄 订阅聚合
- **多机场支持** - 统一管理多个机场订阅
- **智能健康检查** - 每 15 分钟自动检测节点延迟和可用性
- **智能过滤** - 自动去重、按地区精选、延迟排序
- **多格式输出** - 自动识别客户端返回 Clash 或 V2Ray 格式

### 🔐 安全防护
- **IP2Ban** - 可配置的登录失败封禁策略
- **会话管理** - 安全的用户认证机制
- **密码强度校验** - 禁用高危用户名，强制密码复杂度
- **bcrypt 加密** - 安全的密码哈希存储

### 📢 告警通知
- **飞书 Webhook** - 机场失效、节点不足及时通知
- **可配置阈值** - 自定义告警触发条件

### 📦 部署优势
- **单二进制文件** - 17MB 可执行文件，包含完整前后端和 Xray-core
- **零外部依赖** - 内嵌 SQLite 和 Xray，无需单独安装代理内核
- **跨平台支持** - Linux/macOS/Windows × amd64/arm64
- **一键生产部署** - 自动化安装器，包含 Caddy HTTPS 和 systemd 服务

## 🚀 5分钟快速开始

### 1. 下载可执行文件

```bash
# 下载最新版本（或从 releases 页面下载）
wget https://github.com/taliove/proxyhub/releases/latest/download/proxyhub
chmod +x proxyhub
```

### 2. 准备配置文件

```bash
# 下载配置示例
wget https://raw.githubusercontent.com/taliove/proxyhub/main/config.example.yaml -O config.yaml

# 默认配置即可使用，后续在 Web 界面配置
```

### 3. 启动服务

```bash
./proxyhub
```

你会看到：
```
========================================
  ProxyHub 已启动
  监听地址:   http://127.0.0.1:8080
========================================
```

### 4. 打开浏览器完成初始化

访问 `http://localhost:8080`，按向导完成：
1. 设置管理员账户（禁止使用 admin）
2. 配置安全策略（IP2Ban）
3. 登录系统

### 5. 添加机场并创建订阅

1. 进入"机场管理" → 添加机场订阅 URL
2. 进入"订阅地址" → 创建订阅地址
3. 复制订阅 URL 到你的代理客户端

完成！🎉

## 🏭 生产环境部署

ProxyHub 提供一键安装器，自动配置 systemd 服务、Caddy HTTPS 反向代理和运维工具。

### 快速部署

```bash
# 交互式安装（推荐）
bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh)
```

安装器将自动:
- 验证系统环境（Ubuntu 22.04+/Debian 12+，amd64/arm64）
- 生成安全的管理员凭证和随机 Site Path
- 配置 Caddy 自动 HTTPS 证书
- 创建 systemd 服务并启动
- 验证健康检查端点

**内嵌 Xray 架构**: ProxyHub 内置 Xray-core 用于节点健康检查（延迟测速 + 真实请求测试）。无需单独下载或配置 Xray，单二进制包含完整功能。

### 运维工具

```bash
proxyhubctl status       # 查看服务状态
proxyhubctl logs         # 查看日志
proxyhubctl backup       # 创建加密备份
proxyhubctl update       # 在线更新（自动备份 + 回滚）
proxyhubctl rotate-path  # 轮换管理后台路径
```

### 完整文档

- **[生产部署指南](docs/DEPLOY.md)** - 详细的安装、备份、更新、卸载流程
- **[安全模型](docs/SECURITY.md)** - 威胁模型、防护措施、安全假设



## 📱 使用场景

### 家庭多设备管理
为家人的不同设备创建独立订阅地址：
- iPhone、iPad、Mac 各一个订阅
- 独立统计每个设备的使用情况
- 可单独启用/禁用某个订阅

### 机场聚合
- 统一管理多个机场订阅
- 自动筛选最优节点
- 机场失效自动告警

### 自建节点备份
- 配置自建节点作为 FailBack
- 确保始终有可用节点
- 与机场节点统一管理

## 🎯 功能一览

### Web 管理界面
- ✅ **仪表盘** - 节点统计、机场状态、订阅概览
- ✅ **订阅地址管理** - 创建、启用/禁用、统计查看
- ✅ **机场管理** - 添加、编辑、删除机场订阅
- ✅ **节点状态** - 实时查看所有节点延迟和可用性
- ✅ **系统设置** - 安全策略、飞书告警配置

### 订阅功能
- ✅ 自动识别客户端类型（Clash/V2Ray）
- ✅ 支持手动指定格式 `?format=clash`
- ✅ 记录拉取统计（IP、次数、User-Agent）
- ✅ 订阅地址可独立启用/禁用

### 节点管理
- ✅ 定时健康检查（延迟测速）
- ✅ 智能过滤（去重、按地区限额）
- ✅ 地理位置识别
- ✅ 支持 VMess、VLess、Trojan、Shadowsocks

### 安全和告警
- ✅ IP2Ban 防暴力破解（可配置）
- ✅ 会话管理
- ✅ 飞书 Webhook 告警
- ✅ 机场失效检测

## 🏗️ 技术架构

### 技术栈

**前端**
- Vue 3.5 + Vite 6
- TypeScript 5
- Element Plus
- Pinia（状态管理）
- Vue Router
- Axios

**后端**
- Go 1.22
- SQLite (modernc.org/sqlite)
- embed.FS（前端嵌入）
- bcrypt（密码加密）

**构建和部署**
- 单二进制部署（17MB）
- Docker 多阶段构建
- 多平台交叉编译

### 架构设计

```
┌─────────────────────────────────────────────────────┐
│                    ProxyHub                         │
│  ┌──────────────────────────────────────────────┐  │
│  │ Vue 3 SPA (嵌入到 Go 二进制)                  │  │
│  │ - 初始化向导  - 仪表盘  - 机场管理            │  │
│  │ - 订阅管理    - 节点状态 - 系统设置           │  │
│  └──────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────┐  │
│  │ HTTP 服务（Go net/http）                      │  │
│  │ - 订阅端点   - 管理 API  - 静态文件服务       │  │
│  └──────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────┐  │
│  │ 后台任务                                       │  │
│  │ - 定时拉取机场  - 健康检查  - 地理位置解析   │  │
│  └──────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────┐  │
│  │ SQLite 数据库                                  │  │
│  │ - 机场配置  - 订阅地址  - 节点数据  - 统计   │  │
│  └──────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
          ↓ HTTPS 订阅拉取
┌─────────────────────────────────┐
│   终端设备（手机、电脑等）        │
│   Clash / V2Ray / Shadowrocket   │
└─────────────────────────────────┘
```

详见 [架构决策记录](docs/adr/)

## 📚 文档

- **[生产部署指南](docs/DEPLOY.md)** - 一键安装、备份、更新、Site Path 轮换、卸载
- **[安全模型](docs/SECURITY.md)** - 威胁模型、防护措施、安全假设前提
- **[架构决策记录](docs/adr/)** - 领域模型和设计决策

## 🛠️ 开发

### 前置要求
- Go 1.22+
- Node.js 20+
- npm

### 克隆项目

```bash
git clone https://github.com/taliove/proxyhub.git
cd proxyhub
```

### 前端开发

```bash
cd web
npm install
npm run dev
# 访问 http://localhost:3000
# API 自动代理到后端 :8080
```

### 后端开发

```bash
go run ./cmd/server
```

### 完整构建

```bash
# 构建前端
cd web && npm install && npm run build && cd ..

# 复制前端到 cmd/server
cp -r dist/web/* cmd/server/web/

# 构建后端（带嵌入式前端）
go build -o dist/proxyhub ./cmd/server
```

或使用 Makefile：

```bash
make build         # 完整构建
make build-all     # 多平台构建
make test          # 运行测试
make clean         # 清理产物
```

### 项目结构

```
proxyhub/
├── cmd/server/          # 主程序
│   ├── main.go
│   ├── embed.go         # 前端嵌入
│   └── web/             # 前端产物（构建时生成）
├── internal/            # 内部包
│   ├── aggregator/      # 聚合调度器
│   ├── alert/           # 告警
│   ├── config/          # 配置
│   ├── filter/          # 过滤
│   ├── generator/       # 订阅生成
│   ├── geoip/           # 地理位置
│   ├── healthcheck/     # 健康检查
│   ├── server/          # HTTP 服务
│   ├── store/           # 数据库
│   └── subscription/    # 订阅解析
├── web/                 # 前端源码
│   ├── src/
│   │   ├── views/       # 视图组件
│   │   ├── api/         # API 客户端
│   │   ├── stores/      # 状态管理
│   │   ├── router/      # 路由
│   │   └── types/       # TypeScript 类型
│   ├── package.json
│   └── vite.config.ts
├── dist/                # 构建产物
├── docs/                # 文档
├── Makefile
├── Dockerfile
└── README.md
```

## 🐳 Docker 部署

### 使用预构建镜像

```bash
docker run -d \
  -p 8080:8080 \
  -v ./data:/data \
  --name proxyhub \
  taliove/proxyhub:latest
```

### 从源码构建

```bash
docker build -t proxyhub:latest .
docker run -d -p 8080:8080 -v ./data:/data proxyhub:latest
```

## 🔒 安全防护

ProxyHub 采用纵深防御策略:

- **环回监听**: 仅监听 `127.0.0.1:8080`，不直接暴露公网
- **Caddy TLS 终止**: 强制 HTTPS，自动 Let's Encrypt 证书
- **Site Path 随机化**: 管理后台随机路径（20-64 字符），降低扫描器噪音
- **高强度密码**: 安装器自动生成 20 字符密码，bcrypt 哈希存储
- **IP2Ban**: 连续登录失败自动封禁 1 小时
- **备份加密**: 自动加密备份归档，基于 Site Path 派生密钥
- **无 Xray 端口暴露**: Xray-core 仅用于健康检查，不监听端口

详见 [SECURITY.md](docs/SECURITY.md)。

## 🐳 Docker 部署（开发环境）

生产环境推荐使用一键安装器（自动配置 systemd + Caddy HTTPS）。

开发/测试环境可使用 Docker:

```bash
docker run -d \
  -p 8080:8080 \
  -v ./data:/data \
  --name proxyhub \
  taliove/proxyhub:latest
```

**注意**: Docker 部署仅适合本地开发。生产环境需配置反向代理和 HTTPS。

## 📊 性能指标

- **内存占用**: 50-128MB（闲置 / 健康检查期间）
- **CPU 占用**: 1-5%（闲置），10-20%（健康检查期间）
- **并发检查**: 30 个节点（可配置）
- **检查间隔**: 15 分钟（可配置）
- **二进制大小**: 约 20MB（包含前端 + Xray-core）

## 🎓 常见问题

### Q: 如何重置管理员密码？
A: 生产环境使用 `proxyhubctl restore` 恢复备份。开发环境可删除 `data.db` 重新初始化。

### Q: 支持哪些代理协议？
A: VMess、VLess、Trojan、Shadowsocks（通过内嵌 Xray-core 支持）。

### Q: 如何添加自建节点？
A: 在管理后台"系统设置"中配置自建节点信息。

### Q: 内嵌 Xray 如何更新？
A: 运行 `proxyhubctl update` 会同时更新 ProxyHub 和 Xray-core。无需单独更新。

更多问题见 [部署文档](docs/DEPLOY.md)


## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

MIT License - 详见 [LICENSE](LICENSE)

---

**ProxyHub** - 让代理订阅管理更简单 🚀

项目地址：https://github.com/taliove/proxyhub

