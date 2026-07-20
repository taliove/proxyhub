---
name: pre-commit
description: 每次 git commit 前必须执行的检查流程(泄密、垃圾文件、构建、消息规范)
---

# 签入前检查(pre-commit)

任何 `git commit` 之前,按顺序执行以下全部步骤。任何一步失败,先修复再提交。

## 1. 内容审查(防垃圾入库)

```bash
git status --short
git diff --cached --stat
```

逐项确认暂存内容,出现以下任何东西立即移出(并视情况加进 `.gitignore`):
- 过程产物:合并分析、实施计划/总结/验证报告、AI 会话笔记、`.scratch/`、`spec-*.md`(功能工作稿,归 `.scratch/spec/`,禁止进 `docs/`)
- 死备份:`*_old.*`、`*_backup.*`
- 运行时产物:`*.db`、`*.log`、`xray_config.json`、`config.yaml`、`dist/`、二进制
- 大文件:任何 >500KB 的非资源文件都要质疑

判断标准:这个文件对"下一个人理解系统"有帮助吗?

### 文档机械检查(docs/ 或 README.md 变更时)

- `docs/` 下出现 `spec-*.md` -> 拦截,移去 `.scratch/spec/`(政策见 CLAUDE.md §3)
- `docs/` 新增文件不符合命名约定(用户向=大写、设计向=`design-*`、决策=`adr/NNNN-*`)-> 拦截,按 doc-writing skill 决策树归位
- `README.md` 出现白名单外段落(技术栈/项目结构/make 命令/架构图等开发者内容)-> 警告,移去 `docs/DEVELOPMENT.md`;白名单见 CLAUDE.md §3

## 2. 泄密扫描

```bash
gitleaks dir --redact=100 .
```

0 条新增发现才可继续。已知误报白名单见 `.gitleaks.toml`(sitepath fixture、XRAY 公开校验和)。
若引入新的测试 fixture:只允许 `example.com` + 全零 UUID 合成值,禁止任何形式的真实节点信息。

## 3. 构建与测试

```bash
make check   # = vet + Go 测试 + shell 套件 + 前端 lint,一把全过
```

改了前端:必须先 `make build`(go:embed,不构建不生效;`make check` 不含前端构建);前端改动还须过 `make lint-frontend`(已含在 `make check` 中,ESLint warn 不阻塞,Prettier 不贴合会失败)。
既有失败 3 处已在 `make test` 中显式隔离(Makefile 有清单),其余必须全绿。
构建/测试只经 make,禁止裸 `go build`/`npm run build`(见 CLAUDE.md §5)。

## 4. 语义审查(Go 代码改动时)

diff 涉及 Go 代码且非 trivial(改逻辑而非改文案)时,在提交前 dispatch `go-reviewer` agent 审查 `git diff HEAD`,拿到 SHIP  verdict 或修掉它报的 CRITICAL/HIGH。机械门禁抓不住的语义问题(真实感凭证、默认路径越界、错误被吞)由它兜底。

## 5. 提交消息

- 纯英文、纯 ASCII(`-` 和 `->`,不用 `—`/`→`)
- 格式:`<type>: <description>`,type ∈ feat/fix/refactor/docs/test/chore/perf/ci
- 一个提交一个语义

## 完成标准

四步全绿,才允许 `git commit`。
