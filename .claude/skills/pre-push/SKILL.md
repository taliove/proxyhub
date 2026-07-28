---
name: pre-push
description: 推送到任何远端前的最终门禁(全量测试、全历史泄密扫描、仓库体检、Check agent 语义安全审查)
---

# 推送前检查(pre-push)

推送到任何远端(GitHub 等)之前执行。**推送是不可逆的公开发布**——历史一旦推出去,任何残留秘密都视为已泄露,只能轮换不能回收。所以本流程比 pre-commit 严格得多。

## 1. 全量测试与完整构建

`make check` 已由 `.githooks/pre-push` 在推送时自动执行;本步骤补充完整构建验证:

```bash
make check   # vet + Go 测试 + shell 套件
make build   # 验证完整构建(前端 + 后端)可用
```

必须全绿,无隔离名单(AGENTS.md §6);失败先定位原因再决定修代码还是修断言。

## 2. 全历史泄密扫描(不是只扫工作区)

```bash
gitleaks git --redact=100 .
git cat-file --batch-all-objects --batch | grep -a -iE 'password|secret|token|uuid' # 人工抽查
```

- gitleaks 结果必须与 `.gitleaks.toml` 白名单完全一致,零新增
- 第二条命令的输出逐条人工确认是 fixture/公开信息

## 3. 仓库体检

```bash
# 无大文件(>1MB 的 blob 必须为 0)
git rev-list --all --objects | git cat-file --batch-check='%(objecttype) %(objectname) %(objectsize) %(rest)' | awk '$1=="blob" && $3>1000000'

# 无敏感文件名
git rev-list --all | while read c; do git ls-tree -r --name-only $c; done | sort -u | grep -iE '\.(db|sqlite|log|pem|key|env)$|config\.yaml$'
```

注意:第二个命令**严禁加 head 截断**(曾因截断漏掉 data.db,教训见仓库记忆)。

## 4. 文件清单终审

```bash
git ls-files
```

对照 AGENTS.md §1/§2 逐类过一遍:无过程文档、无死备份、无运行时产物、无依赖目录。
文档只应有:术语表/ADR/设计/运维。

## 5. 语义安全审查(Check agent)

机械扫描抓不住的攻击面问题,由独立上下文的 `check` agent(push 模式)兜底。推送前必须 dispatch 它审查本次推送的增量:

- 基线:`git merge-base origin/<目标分支> HEAD`;首次推送(无 upstream)退化为以空树为基线的全量审查
- 视角:这段历史推出去后,公开世界里谁能利用什么——日志/错误泄节点凭证、无鉴权新端点、订阅地址可枚举、管理面绑非回环、TLS 红线、前端 token 外泄
- 拿到 SHIP verdict,或修掉它报的 CRITICAL/HIGH 后再推;MEDIUM/LOW 不阻断但要逐条过目

## 6. 发布要件(首次推送 public 仓库时)

- [ ] LICENSE 文件存在且与项目匹配
- [ ] README 与实际功能一致(语言、截图不含真实数据)
- [ ] CI(`.github/workflows/`)里有 gitleaks 扫描 job
- [ ] 远端是 private 还是 public 已明确确认

## 完成标准

六项全过才允许 `git push`。任何一项存疑,停下来问,不许赌。
