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
- 过程产物:合并分析、实施计划/总结/验证报告、AI 会话笔记、`.scratch/`
- 死备份:`*_old.*`、`*_backup.*`
- 运行时产物:`*.db`、`*.log`、`xray_config.json`、`config.yaml`、`dist/`、二进制
- 大文件:任何 >500KB 的非资源文件都要质疑

判断标准:这个文件对"下一个人理解系统"有帮助吗?

## 2. 泄密扫描

```bash
gitleaks dir --redact=100 .
```

0 条新增发现才可继续。已知误报白名单见 `.gitleaks.toml`(sitepath fixture、XRAY 公开校验和)。
若引入新的测试 fixture:只允许 `example.com` + 全零 UUID 合成值,禁止任何形式的真实节点信息。

## 3. 构建与测试

```bash
make check   # = vet + Go 测试 + shell 套件,一把全过
```

改了前端:必须先 `make build`(go:embed,不构建不生效;`make check` 不含前端构建)。
既有失败 3 处已在 `make test` 中显式隔离(Makefile 有清单),其余必须全绿。
构建/测试只经 make,禁止裸 `go build`/`npm run build`(见 CLAUDE.md §5)。

## 4. 提交消息

- 纯英文、纯 ASCII(`-` 和 `->`,不用 `—`/`→`)
- 格式:`<type>: <description>`,type ∈ feat/fix/refactor/docs/test/chore/perf/ci
- 一个提交一个语义

## 完成标准

四步全绿,才允许 `git commit`。
