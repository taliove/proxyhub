---
name: dev-workflow
description: ProxyHub 日常开发统一范式——编译、运行、测试、文件归属(CLAUDE.md §2 目录基本法的执行流程)
---

# 日常开发范式(dev-workflow)

一切日常开发动作按本流程执行。目录归属的最终依据是 `CLAUDE.md` §2,本文件是操作层。

## 1. 写文件之前:先定归属

| 你要写的东西 | 放哪 | 禁止 |
|---|---|---|
| Go 源码 | `internal/<领域>/`(按领域分包);入口薄 main 在 `cmd/server/` | 根目录、新建顶层目录(先问) |
| 单元测试 | 与源码同包 `xxx_test.go`;fixture 放包内 `testdata/` | 跨包测试文件、真实凭证 fixture |
| 前端源码 | `web/src/`(构建产物由 vite 写入 `cmd/server/web/`,gitignored) | 手改 `cmd/server/web/` 产物 |
| 运维脚本 | `scripts/install/` 或 `scripts/release/`,配套 `test_*.sh` | 根目录散落 .sh |
| 编译产物 | `dist/`(make 已保证) | 根目录 `go build` 裸跑(会生成 `./proxyhub`、`./server`) |
| 运行态(数据库/日志/生成的 xray 配置) | `var/data`、`var/log`、`var/xray`(代码默认路径必须落这里,写入方负责 `MkdirAll`) | 任何写到仓库根的默认路径 |
| 测试临时数据 | `.test/`(集成)/ `t.TempDir()`(单元) | 提交 `.test/` 内容 |
| 文档 | 按 `CLAUDE.md` §3:术语/ADR/设计/运维 | 过程产物(计划/总结/验证报告) |

口诀:**根目录只放入口与控制文件;产物进 dist,运行态进 var,测试临时进 .test/testdata。**

## 2. 编译

```bash
make build          # 唯一正确姿势:web npm build → go build -o dist/proxyhub
```

- 改了前端(`web/`):必须 `make build` 完整跑(go:embed,单独 go build 不会更新前端)
- 只改 Go:`go build -o dist/proxyhub ./cmd/server` 即可,不要裸 `go build`(产物会掉在根目录)
- 多平台发布:`make build-all`(产物也在 `dist/`)

## 3. 运行与调试

```bash
./start.sh          # 后台启动 dist/proxyhub,日志 var/log/proxyhub.log,pid 同目录
tail -f var/log/proxyhub.log
make dev-frontend   # 前端热更新开发(vite dev server)
make dev-backend    # 后端开发(config.example.yaml,storage 指向 var/data/data.db)
```

重启验证三件套:`curl -s -o /dev/null -w "%{http_code}" http://localhost:8080/` 返回 200;`var/xray/xray_config.json` 已重新生成;日志无新增 ERROR(已知:`enabled_nodes: 0` 时 distribution 起不来是既有功能 bug,不算新问题)。

## 4. 测试

```bash
go test ./...                                  # 全量
go test ./internal/<改动的包>/                  # 日常最小集
cd scripts/install && bash test_<对应套件>.sh   # 改了安装/CLI 时
```

- 新功能先写测试(TDD):同包 `_test.go`,fixture 必须合成值(`example.com` + 全零 UUID)
- 既有失败 3 处(2 模板 + TestHandleTestNode_MissingTarget)不是回归,别"修"
- 集成测试产生的数据只能落在 `.test/`

## 5. 提交之前

调用 `pre-commit` skill。本 skill 只管开发范式,签入门禁以 pre-commit 为准。

## 完成标准

归属正确 + 构建过 + 测试过 + 根目录无新增散件(`git status` 里不该出现根目录新文件,除非它本就是入口/控制文件)。
