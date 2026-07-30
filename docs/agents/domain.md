# 领域文档

工程 skill 探索代码库时应如何消费本仓库的领域文档。

## 探索前必读

- 仓库根的 **`CONTEXT.md`**;或根的 **`CONTEXT-MAP.md`**(若存在)——它指向每个 context 各自的 `CONTEXT.md`,读与主题相关的那些。
- **`docs/adr/`**——读与你将要触碰的区域相关的 ADR。多 context 仓库还要查 `src/<context>/docs/adr/` 里的 context 级决策。

若这些文件不存在,**静默继续**。不要指出其缺席,也不要建议预先创建。`/domain-modeling` skill(经 `/grill-with-docs` 与 `/improve-codebase-architecture` 到达)会在术语或决策真正收敛时惰性创建它们。

## 文件结构

单 context 仓库(大多数仓库):

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-event-sourced-orders.md
│   └── 0002-postgres-for-write-model.md
└── src/
```

多 context 仓库(根目录存在 `CONTEXT-MAP.md`):

```
/
├── CONTEXT-MAP.md
├── docs/adr/                          ← 系统级决策
└── src/
    ├── ordering/
    │   ├── CONTEXT.md
    │   └── docs/adr/                  ← context 级决策
    └── billing/
        ├── CONTEXT.md
        └── docs/adr/
```

## 使用词汇表的术语

当你的输出命名一个领域概念时(issue 标题、重构提案、假设、测试名),使用 `CONTEXT.md` 定义的术语,不要漂移到词汇表明确避免的同义词。

如果你需要的概念不在词汇表里,这是一个信号——要么你在发明项目不用的语言(重新考虑),要么存在真实的空缺(记给 `/domain-modeling`)。

## 标记 ADR 冲突

如果你的输出与既有 ADR 矛盾,显式指出而不是静默覆盖:

> _与 ADR-0007(event-sourced orders)矛盾——但值得重开讨论,因为…_
