---
name: zh-punct
description: 中文文档标点规整——用户向文档(README/FAQ/DEPLOY/SECURITY)的中文行文半角标点转全角,保护代码块/行内代码/URL/链接。写文档、改文档、pre-commit 发现 docs 变更时执行;凡是动过用户向文档,提交前必须过一遍。
---

# 标点规整(zh-punct)

中文行文用全角标点,是用户向文档的成文约定(AGENTS.md §3)。本 skill 用脚本机械执行,不靠手感。

## 何时执行

- 新建或修改了用户向文档(README.md、docs/FAQ.md、docs/DEPLOY.md、docs/SECURITY.md 及未来同类大写文件)之后、commit 之前;
- 发现文档里中文行文混入半角标点时。

开发者向文档(CONTEXT.md、docs/design-*、docs/adr/、docs/DEVELOPMENT.md)维持半角行文风格,**不要**对它们跑本脚本(ADR 不可变)。

## 怎么跑

```bash
# 指定文件(就地改写,幂等)
python3 .claude/skills/zh-punct/scripts/zh_punct.py README.md docs/FAQ.md

# 四个用户向文档一把过
python3 .claude/skills/zh-punct/scripts/zh_punct.py --user-facing

# 检查模式(不改写,有需要规整的文件时退出码 1,可用于门禁)
python3 .claude/skills/zh-punct/scripts/zh_punct.py --check --user-facing
```

跑完必须 `git diff` 过目:脚本是机械规则,边界 case(新出现的保护形态)靠人眼兜底。

## 规则边界(改动脚本前先读)

- **不动**:围栏代码块 ``` 内的一切(含块内中文注释与示例输出)、行内代码、Markdown 链接目标、URL。
- **转**:中文后的 `,` `;` `:` `?` `!` → 全角;含中文且紧随中文的圆括号对 → 全角;历史混杂括号对按左括号宽度归一。
- **不转**:纯英文/技术内容的括号(`(GHCR)`、`(--no-caddy)`)、英文句、数字与版本号语境。
- 脚本幂等,可以反复跑;正常状态下 `--check` 应全部干净。
