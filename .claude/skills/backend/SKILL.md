---
name: backend
description: ProxyHub Go 后端开发范式——改 internal//cmd/ 代码、文件归属、构建与测试纪律时执行(AGENTS.md §2 目录基本法与 §5 make 入口的执行流程)
---

# 后端开发范式(backend)

目录归属的最终依据是 `AGENTS.md` §2,构建/测试入口的最终依据是 §5,本 skill 是操作层。

## 1. 写文件之前:先定归属

| 你要写的东西 | 放哪 | 禁止 |
|---|---|---|
| Go 源码 | `internal/<领域>/`(按领域分包);入口薄 main 在 `cmd/server/` | 根目录、新建顶层目录(先问) |
| 单元测试 | 与源码同包 `xxx_test.go`;fixture 放包内 `testdata/` | 跨包测试文件、真实凭证 fixture |
| 前端源码 | `web/src/`(构建产物由 vite 写入 `cmd/server/web/`,gitignored) | 手改 `cmd/server/web/` 产物 |
| 运维脚本 | `scripts/install/` 或 `scripts/release/`,配套 `test_*.sh` | 根目录散落 .sh |
| 编译产物 | `dist/`(make 已保证) | 根目录 `go build` 裸跑(会生成 `./proxyhub`、`./server`) |
| 运行态(数据库/日志) | `var/data`、`var/log`(代码默认路径必须落这里,写入方负责 `MkdirAll`) | 任何写到仓库根的默认路径 |
| 测试临时数据 | `.test/`(集成)/ `t.TempDir()`(单元) | 提交 `.test/` 内容 |
| 文档 | 按 `AGENTS.md` §3:术语/ADR/设计/运维 | 过程产物(计划/总结/验证报告) |

口诀:**根目录只放入口与控制文件;产物进 dist,运行态进 var,测试临时进 .test/testdata。**

## 2. 编译(唯一入口:make)

```bash
make build          # 完整构建:前端 npm build → 后端 go build -o dist/proxyhub
make build-backend  # 只改了 Go
```

**禁止任何形式的裸 `go build`**——裸 `go build ./cmd/server`(无 `-o`)会把 `server` 二进制掉在根目录(历史事故);裸 `go test -c` 会掉 `*.test`。产生文件的动作只经 make。

## 3. 测试(唯一入口:make)

```bash
make test         # Go 全量
make test-shell   # 安装/运维脚本六套件
make check        # 签入前聚合:vet + test + test-shell + lint-frontend
```

唯一豁免:定向调试允许 `go test ./internal/<pkg>/ -run <TestName>`(纯读操作,不落盘)。

- 新功能先写测试(TDD):同包 `_test.go`,fixture 必须合成值(`example.com` + 全零 UUID)
- 测试套件保持全绿,**无隔离名单**(AGENTS.md §6);发现失败先定位原因,再决定修代码还是修断言,不许用 `-skip` 掩盖回归
- 集成测试产生的数据只能落在 `.test/`

## 4. 数据库相关改动

涉及 schema/迁移/数据路径时,同时执行 `database` skill。

## 5. 提交之前

调用 `pre-commit` skill。本 skill 只管开发范式,签入门禁以 pre-commit 为准。

## 完成标准

归属正确 + 构建过 + 测试过 + 根目录无新增散件(`git status` 里不该出现根目录新文件,除非它本就是入口/控制文件)。
