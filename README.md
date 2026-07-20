# ProxyHub

把多个机场订阅聚合成一个统一订阅地址 - 自动筛选最优节点,一个链接喂饱所有设备。

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GitHub Release](https://img.shields.io/github/v/release/taliove/proxyhub)](https://github.com/taliove/proxyhub/releases)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![Vue Version](https://img.shields.io/badge/Vue-3.5-4FC08D?logo=vue.js)](https://vuejs.org/)

## 核心特性

- **订阅聚合** - 统一管理多个机场订阅,输出一个聚合订阅地址
- **智能筛选** - 定时健康检查、自动去重、按地区精选、延迟排序
- **多格式输出** - 自动识别客户端,返回 Clash 或 V2Ray 格式
- **Web 管理界面** - 初始化向导、仪表盘、机场/节点/订阅管理,全程图形化操作
- **自建节点兜底** - 自建节点作为 FailBack,与机场节点统一管理
- **告警通知** - 机场失效、节点不足时通过飞书 Webhook 及时通知
- **单二进制部署** - 内嵌前端、SQLite 与 Xray-core,零外部依赖

## 快速开始

### 1. 下载可执行文件

```bash
wget https://github.com/taliove/proxyhub/releases/latest/download/proxyhub
chmod +x proxyhub
```

### 2. 准备配置文件

```bash
wget https://raw.githubusercontent.com/taliove/proxyhub/main/config.example.yaml -O config.yaml
# 默认配置即可使用,后续在 Web 界面配置
```

### 3. 启动服务

```bash
./proxyhub
```

### 4. 打开浏览器完成初始化

访问 `http://localhost:8080`,按向导完成:
1. 设置管理员账户
2. 配置安全策略(IP2Ban)
3. 登录系统

### 5. 添加机场并创建订阅

1. 进入"机场管理" → 添加机场订阅 URL
2. 进入"订阅地址" → 创建订阅地址
3. 复制订阅 URL 到你的代理客户端

完成!🎉

## 生产部署

一键安装器自动配置 systemd 服务、Caddy HTTPS 反向代理和运维工具:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/taliove/proxyhub/main/install.sh)
```

安装后使用 `proxyhubctl` 运维:`status` / `logs` / `backup` / `update` / `rotate-path`。

详细安装、备份、更新、卸载流程见 **[生产部署指南](docs/DEPLOY.md)**。

### Docker(开发/测试环境)

```bash
docker run -d \
  -p 8080:8080 \
  -v ./data:/data \
  --name proxyhub \
  taliove/proxyhub:latest
```

> Docker 部署仅适合本地开发。生产环境请使用一键安装器(自动配置 HTTPS)。

## 常见问题

- **支持哪些代理协议?** VMess、VLess、Trojan、Shadowsocks(内嵌 Xray-core)。
- **订阅拉取失败 "no available nodes"?** 等待首次健康检查完成(启动后约 15 分钟)。
- **忘记管理员密码?** 可通过备份恢复或手动重置数据库。
- **如何添加自建节点?** 管理后台"系统设置"中配置。

更多问题见 **[FAQ](docs/FAQ.md)**。

## 安全

ProxyHub 采用纵深防御:仅监听环回地址、Caddy 强制 HTTPS、管理后台随机路径、登录失败自动封禁、备份加密。

威胁模型与防护措施详见 **[安全模型](docs/SECURITY.md)**。

## 文档

**使用者**
- [生产部署指南](docs/DEPLOY.md) - 安装、备份、更新、卸载
- [常见问题](docs/FAQ.md) - 使用与运维问答
- [安全模型](docs/SECURITY.md) - 威胁模型与防护措施

**开发者**
- [开发指南](docs/DEVELOPMENT.md) - 环境搭建与开发循环

## 贡献

欢迎提交 Issue 和 Pull Request!

## 许可证

MIT License - 详见 [LICENSE](LICENSE)

---

项目地址:https://github.com/taliove/proxyhub
