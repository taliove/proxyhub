# Spec: 节点管理 —— 自建节点 CRUD + 机场节点屏蔽 + 关键词白名单

> 决策依据：ADR 0009、CONTEXT.md。grilling 已钻透。

## Problem Statement

节点页只有只读状态表，用户无法管理节点。具体三个缺口：

1. **自建节点无法在后台维护**：store 层埋了一半（有增/删/查，无改/启停），且没有 API、没有前端页面。用户想加个自建节点作 FailBack 只能改数据库。
2. **机场节点无法手动剔除坏节点**：机场偶尔混入个别连不通/被墙的节点，用户想拉黑单个，但机场节点每轮刷新整体替换，手动删下轮就被冲回来。
3. **过滤只能排除、不能挑选**：系统设置只有关键词黑名单。用户的真实场景是"我只要香港/新加坡/美国/日本这几个地区"，一个个拉黑几十个不要的地区太累，需要白名单只留想要的。

## Solution

- **自建节点四件套**：增/改/删/启停，前端表单按协议动态显示字段。
- **机场节点屏蔽**：按 `server:port`（NodeKey）精确拉黑单个机场节点，跨刷新持久；订阅生成时剔除；可取消屏蔽。
- **关键词白名单**：与黑名单同套机制（子串、不区分大小写、订阅时生效、自建豁免），方向相反——非空时只保留命中的机场节点。
- **过滤链**：订阅生成时依次 白名单 → 黑名单 → 机场屏蔽 → 自建豁免。

## User Stories

1. 作为用户，我想在后台添加自建节点，这样可以把常驻节点作为 FailBack 注入池子。
2. 作为用户，我想编辑已有自建节点的参数，这样节点信息变更时不用删了重建。
3. 作为用户，我想删除自建节点，这样淘汰的节点不再出现在订阅里。
4. 作为用户，我想临时停用某个自建节点而不删除它，这样节点维护期间可以先停，恢复后再启用。
5. 作为用户，我想在新增/编辑自建节点时只看到当前协议相关的字段，这样不会给 trojan 误填 cipher。
6. 作为用户，我想在管理页看到所有自建节点（含已禁用的），这样能对禁用的重新启用。
7. 作为用户，我想屏蔽某个连不通的机场节点，这样它不再出现在订阅里。
8. 作为用户，我想屏蔽的节点在下一轮刷新后依然被屏蔽，这样不用每轮重新操作。
9. 作为用户，我想取消对某节点的屏蔽，这样它恢复后能重新进订阅。
10. 作为用户，我想在节点页看到每个节点当前是否被屏蔽，这样知道操作状态。
11. 作为用户，我想设置关键词白名单只保留想要的地区，这样订阅里只有香港/新加坡/美国/日本这类我要的节点。
12. 作为用户，我想白名单为空时保留全部节点，这样不设白名单就等于不启用。
13. 作为用户，我想白名单和黑名单同时生效（白名单挑选后黑名单再排除），这样能"只要某地区但排除该地区的假节点"。
14. 作为用户，我想我的自建节点不被任何过滤（白名单/黑名单/屏蔽）影响，这样 FailBack 安全网始终可靠。
15. 作为用户，我想后台订阅预览与实际 /sub 输出一致，这样预览所见即所得。

## Implementation Decisions

### 自建节点（internal/store, internal/server, web）
- store 补 `UpdateSelfHostedNode(node *SelfHostedNode) error` 和 `SetSelfHostedNodeEnabled(id int64, enabled bool) error`。
- `ListSelfHostedNodes` 现仅返回 `enabled=1`；聚合注入仍用它，但管理页需要全部——新增 `ListAllSelfHostedNodes()` 返回含禁用的全集（供管理页），聚合路径继续用只返回启用的旧方法。
- API：`GET/POST /api/self-nodes`、`PUT/DELETE /api/self-nodes/{id}`、`POST /api/self-nodes/{id}/toggle`，全部 requireAuth。
- 前端 `SelfNodes.vue`：参照 `Airports.vue` 的表格 + 弹窗表单 + 启停开关；表单按 `protocol` 动态字段（ss→cipher/password；trojan→password；vmess→uuid/alterId/cipher/network/tls；vless→uuid/network/tls；name/server/port 共有）。路由 `/self-nodes` + 导航入口。

### 机场节点屏蔽（新增持久名单）
- 新建 `node_blocks` 表：`node_key TEXT PRIMARY KEY, created_at TIMESTAMP`。选表而非 settings-JSON，因屏蔽项独立增删、需主键去重。
- store：`BlockNode(nodeKey string) error`（INSERT OR IGNORE）、`UnblockNode(nodeKey string) error`、`ListBlockedNodes() (map[string]bool, error)`。
- API：`POST /api/nodes/block`、`POST /api/nodes/unblock`，body `{"node_key": "server:port"}`。
- `toNodeViews` 增加 `NodeKey` 与 `Blocked` 字段（需传入屏蔽名单以标记）；`handleListNodes` 读屏蔽名单后标记。
- 订阅生成剔除命中 NodeKey 的机场节点；自建节点豁免。

### 关键词白名单（internal/filter, settings）
- filter 包新增 `FilterByWhitelist(nodes, keywords)`：keywords 为空返回原样；非空只保留名称命中任一关键词的机场节点，自建节点（`Source == SourceSelfHosted`）始终保留。复用 `SplitKeywords`。
- settings 新增键 `filter_whitelist`，复用 `GetSystemSettings/SaveSystemSettings`。
- 前端 `Settings.vue` 增白名单输入项。

### 过滤链组合（internal/server）
- `keywordFilteredNodes` 演进为组合过滤 `filteredNodes`（或保留名字但扩展）：读 settings（白名单+黑名单）+ 屏蔽名单，依次应用：
  1. 白名单（非空则只留命中）
  2. 黑名单（剔除命中）
  3. 机场屏蔽（剔除 NodeKey 命中）
  - 自建节点全程豁免（在每道过滤内部判断 Source）。
- `handleSubscription` 与 `handleEndpointPreview` 共用这条链（ADR 0005 所见即所得）。
- 读 settings / 屏蔽名单失败时降级：跳过对应过滤而非整体失败（延续现有 fail-open）。

### Schema Changes
```sql
CREATE TABLE IF NOT EXISTS node_blocks (
    node_key   TEXT PRIMARY KEY,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### API Contracts
- `GET /api/self-nodes` → `{nodes: [...含 enabled...]}`
- `POST /api/self-nodes` body=SelfHostedNode → 201/200
- `PUT /api/self-nodes/{id}` body=SelfHostedNode → 200
- `DELETE /api/self-nodes/{id}` → 200
- `POST /api/self-nodes/{id}/toggle` → 200
- `POST /api/nodes/block` `{node_key}` → 200
- `POST /api/nodes/unblock` `{node_key}` → 200
- `GET /api/nodes` → `{last_update, nodes: [{name,type,region,source,latency,available,node_key,blocked}]}`
- settings 读写沿用 `GET/POST /api/settings`，payload 增 `filter_whitelist`

## Testing Decisions

好测试只验外部行为，不验实现细节。沿用现有 seam。

- **filter 包单元测试**（prior art: `internal/filter/keyword_test.go`）：`FilterByWhitelist` —— 空白名单=全保留；非空=只留命中；自建节点豁免；大小写不敏感。
- **store 单元测试**（prior art: `internal/store/store_test.go`）：`UpdateSelfHostedNode`/`SetSelfHostedNodeEnabled` 读写回环；`BlockNode`/`UnblockNode`/`ListBlockedNodes`（幂等、取消、持久）。
- **server HTTP 集成测试**（prior art: `internal/server/server_test.go` newTestServer+authCookie+httptest）：
  - 自建节点 CRUD + toggle 全流程；
  - 屏蔽某节点后 `/sub` 不含它，取消后恢复；
  - 白名单非空时 `/sub` 只含命中机场节点、自建节点始终在；
  - 组合过滤顺序（白名单+黑名单+屏蔽叠加）；
  - 预览与 `/sub` 一致。

## Out of Scope

- 审计功能（登录/IP 封禁可视化）—— 已记 backlog，后续 grill。
- 订阅地址统计 —— 已记 backlog，后续 grill。
- 地区多选白名单（地区识别只覆盖 7 个 HK/TW/JP/SG/US/KR，故用关键词白名单）。
- 订阅格式改动（维持按 UA 自选：Clash→模板 YAML，V2Ray→base64）。
- 机场节点的编辑（机场节点是刷新产物，改了下轮被冲；只支持屏蔽）。

## Further Notes

- 屏蔽用 NodeKey（`server:port`）而非节点名：名称可能重复且会变，`server:port` 才是稳定标识。
- 自建节点豁免必须在**每一道**过滤里落实（白名单/黑名单/屏蔽），不能只在某一道。
- 无 git remote，本 spec 作为本地实现依据（同 docs/spec-editable-clash-template.md 的处理）。
