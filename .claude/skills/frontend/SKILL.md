---
name: frontend
description: ProxyHub 前端开发范式——改 web/(Vue/TS)代码时的构建纪律(go:embed)、两条开发回路、lint 与验证流程。改前端必执行。
---

# 前端开发范式(frontend)

前端 SPA 源码在 `web/src/`,vite 构建产物写入 `cmd/server/web/`(gitignored)并 **go:embed 进二进制**——这一事实决定了下面所有纪律。设计约束见 `docs/design-frontend.md`。

## 1. 归属

- 源码只进 `web/src/`;**禁止手改 `cmd/server/web/` 构建产物**
- 前端 lint 唯一入口:`make lint-frontend`(ESLint + Prettier + 类型检查;warn 不阻塞,Prettier 不贴合会失败)

## 2. 两条开发回路,按目的选

- **迭代回路**(边改边看):后端一个实例(`make start`)+ `make dev-frontend`(vite dev server,HMR,代理 /api 与 /sub 到 8080)。前端改动秒级生效,不用 build 不用重启;改后端则 `make restart`。
- **验证回路**(验收嵌入产物):`make build && make restart`。go:embed 决定了 dev server 验证不了最终二进制里的前端,**汇报完成前这一步不可省**——改了前端却只跑 `make build-backend` = 没生效。

## 3. 前端改动完整闭环

```bash
make lint-frontend   # 改前端必跑
make build           # 完整构建(go:embed,make check 不含前端构建,必须单独跑)
make restart         # 重启生效
make status          # 确认进程状态
```

重启验证:`curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/` 返回 200;`var/log/` 无新增 ERROR;页面/接口实测确认改动实际呈现(验证纪律见 AGENTS.md §5)。

## 4. 提交之前

调用 `pre-commit` skill(它会查 `make check` 与前端构建)。
