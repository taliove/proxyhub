# 订阅动态查询(节点范围条件) - 设计

## 概述

不同订阅地址可配置不同的**节点范围**:一组结构化筛选条件(机场/地区/标签/关键词)。
拉取订阅时在**当前那一刻**对内存节点池按条件求值,得出该地址应下发的节点子集,
不落静态清单。因此机场刷新后订阅自动跟随,永不腐化。条件为空=全量(现状行为,零回归)。

本文描述"是什么、怎么运作",随代码演进。谓词求值实现见 `internal/subfilter`。

## 条件模型

条件存 `endpoints.conditions` 列(TEXT,JSON;空串=不筛选)。迁移见
`internal/store/migrations/013_endpoint_conditions.sql`(经 `addColumnIfMissing` 施加,
理由同 011:ADD COLUMN 的幂等标记是列存在性,不能走表存在性标记的 applyMigrationFile)。

```go
type Conditions struct {
    Airports []string // 机场名(node.Source)
    Regions  []string // 地区码(node.Region,如 HK/US)
    Tags     []string // 自动标签(node_tags 表)
    Keyword  string   // 节点名子串,大小写不敏感
}
```

## 谓词语义(与前端 `web/src/views/nodes/predicates.ts` 的对齐契约)

纯函数,对每个节点独立判定,**四维度全满足才命中(跨维度 AND)**:

| 维度 | 匹配对象 | 维度内语义 | 大小写 |
|---|---|---|---|
| Airports | `node.Source` | OR(任一机场命中) | 精确 |
| Regions | `node.Region` | OR(任一地区命中) | 精确 |
| Tags | `node_tags` 标签集 | **AND(全含才命中)** | 精确 |
| Keyword | `node.Name` 子串 | 单值 | 不敏感(折叠) |

**为何维度内 OR/AND 不同**:一个节点只有单一 Source/Region,多选机场/地区只能表达"任一"(OR);
一个节点可带多个标签,多选标签取"全含"(AND)才能表达"US 且 Netflix 且稳定"这类交集意图。

**Regions 用识别地区码**(recognizer 从节点名识别的 HK/JP/US),不是出网国家码;
出网国家码走 `region:<CC>` 标签维度(来自体检出网段,见下)。二者是不同数据源的不同过滤。

**标签词表**由 `internal/nodetag/derive.go` 固定:解锁(`nf-full`/`nf-originals`/`yt-premium`/
`disney-plus`/`openai`/`claude`/`gemini`)、稳定(`stable-good`/`fair`/`poor`)、
出网质量(`fast`/`ipv6`/`hosting`/`residential`/`dns-leak`/`region:<CC>`)。
前端 UI 把解锁/稳定/出网分组呈现,`region:<CC>` 等动态标签允许自行输入(allow-create),
全部写入同一 `tags` 维度。

## 求值接入点(所见即所得)

`/sub` 与后台预览共用过滤链,保证 WYSIWYG:

```
内存池 -> filteredNodes(白/黑名单/屏蔽/可用性/延迟/去重) -> applyConditions(本地址条件) -> 标准化 -> 生成
```

- `applyConditions` 在**全局过滤链之后**收窄:条件在已可下发的节点上进一步取范围。
- 空条件短路返回原池(零回归)。
- **自建节点不豁免条件**:全局卫生过滤(白/黑名单/屏蔽)里自建是 FailBack 安全网而豁免,
  但 conditions 是用户对本地址的**显式取范围意图**,应可预测地收窄。默认(空条件)完整保留
  含自建 FailBack 的现状。
- 标签数据按需拉取(仅当条件含 tag 维度);拉取失败降级为**丢弃 tag 维度、保留其余维度**
  (与过滤链"读不出就跳过、宁可多给"的降级风格一致),而非把订阅打空。

## 相关端点

| 端点 | 用途 |
|---|---|
| `PUT /api/endpoints/{id}/conditions` | 保存某地址的条件(请求体即 Conditions;空条件落库为空串) |
| `POST /api/endpoints/preview-conditions` | 预览一组未保存条件的命中数(`{count,total}`),编辑时实时反馈 |
| `GET /api/endpoints/{id}/preview` | 预览该地址实际下发内容(已套用条件) |
