---
name: pre-push
description: 推送到任何远端前的最终门禁(全量测试、全历史泄密扫描、仓库体检)
---

# 推送前检查(pre-push)

推送到任何远端(GitHub 等)之前执行。**推送是不可逆的公开发布**——历史一旦推出去,任何残留秘密都视为已泄露,只能轮换不能回收。所以本流程比 pre-commit 严格得多。

## 1. 全量测试

```bash
go build ./... && go vet ./...
go test ./...
cd scripts/install && for t in test_*.sh; do bash "$t" || exit 1; done
cd ../../web && npm run build
```

既有失败(2 个模板测试 + TestHandleTestNode_MissingTarget)除外,其余必须全绿。

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

对照 CLAUDE.md §1/§2 逐类过一遍:无过程文档、无死备份、无运行时产物、无依赖目录。
文档只应有:术语表/ADR/设计/运维。

## 5. 发布要件(首次推送 public 仓库时)

- [ ] LICENSE 文件存在且与项目匹配
- [ ] README 与实际功能一致(语言、截图不含真实数据)
- [ ] CI(`.github/workflows/`)里有 gitleaks 扫描 job
- [ ] 远端是 private 还是 public 已明确确认

## 完成标准

五项全过才允许 `git push`。任何一项存疑,停下来问,不许赌。
