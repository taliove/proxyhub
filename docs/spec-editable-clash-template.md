# Spec: Editable Clash Configuration Template

## Problem Statement

订阅地址当前生成的 Clash 配置过于简化（仅2个策略组+1条 MATCH 规则），缺少实用的分流规则、DNS配置、应用分组等。用户希望订阅输出包含完整的生产级配置（hosts/dns/proxy-groups/rules），类似专业机场提供的16k行配置文件，同时保留在后台可视化编辑的能力。

当前痛点：
1. 订阅地址输出的配置无法满足日常使用需求（没有应用分流、DNS优化等）
2. 用户需要手动合并机场订阅与自定义规则，维护成本高
3. 配置硬编码在代码里，无法根据个人需求调整

## Solution

实现可编辑的 Clash 配置模板系统：
1. **默认富模板**：从用户的参考配置文件提取完整的 hosts/dns/proxy-groups（41个应用分组）/rules（数百条域名分流规则）作为开箱即用的默认模板
2. **动态节点注入**：模板中使用 `{{nodes}}` 占位符，订阅生成时动态替换为当前聚合的所有节点名
3. **后台可视化编辑**：Web 界面提供 Monaco Editor（VS Code 编辑器）编辑完整 YAML 模板，支持语法高亮和基础错误提示
4. **持久化存储**：用户修改的模板存入 SQLite（settings 表），可随时恢复为默认模板

## User Stories

1. 作为 ProxyHub 用户，我希望订阅地址生成的配置包含完整的应用分流规则（OpenAI/Claude/YouTube/Netflix等），这样我不需要手动配置就能按应用选择节点
2. 作为用户，我希望订阅配置包含优化的 DNS 设置（fake-ip + DoH），这样可以加速域名解析并防止 DNS 泄漏
3. 作为用户，我希望订阅配置包含 hosts 映射，这样可以解决某些域名的解析问题
4. 作为用户，我希望在后台可视化编辑配置模板，这样可以根据个人需求调整分流规则和策略组
5. 作为用户，我希望模板编辑器提供 YAML 语法高亮，这样可以更容易发现格式错误
6. 作为用户，我希望保存模板前有格式验证，这样不会因为 YAML 语法错误导致订阅失效
7. 作为用户，我希望可以一键恢复默认模板，这样改坏了可以快速回退
8. 作为用户，我希望模板中的节点是动态注入的，这样每次聚合拉取新节点后订阅自动更新
9. 作为用户，我希望可以在模板中定义多个策略组（手动切换/地区分组/应用分组），这样可以灵活控制流量走向
10. 作为用户，我希望模板支持引用其他策略组（如应用组引用"手动切换"组），这样可以复用节点选择
11. 作为用户，我希望订阅配置的 proxies 字段由聚合系统自动生成，这样节点的完整配置（server/port/uuid等）始终是最新的
12. 作为用户，我希望所有订阅地址共享同一套模板，这样只需维护一份配置
13. 作为用户，我希望模板修改立即生效，这样不需要重启服务
14. 作为用户，我希望默认模板包含常用的分流规则（国内直连/国外代理/广告屏蔽等），这样开箱即用
15. 作为用户，我希望模板编辑器有加载/保存状态提示，这样知道操作是否成功
16. 作为开发者，我希望占位符语法简单明确（`{{nodes}}`），这样容易理解和维护
17. 作为开发者，我希望模板验证逻辑能捕获常见错误（YAML格式错误/占位符拼写错误），这样可以给用户清晰的错误提示
18. 作为开发者，我希望默认模板通过 go:embed 打包进二进制，这样部署时不需要额外的配置文件

## Implementation Decisions

### Architecture

**Template Storage & Loading**
- settings 表（已存在）存储用户编辑的模板，key='clash_template'
- 默认模板作为 `internal/generator/default_template.yaml` 通过 `//go:embed` 打包进二进制
- 首次启动或用户点击"恢复默认"时，从 embed 资源加载到 DB
- 订阅生成时始终从 DB 读取模板（GetSetting），保证修改立即生效

**Subscription Generation Flow (重构)**
- 旧流程：`GenerateClash(nodes)` → 硬编码生成 YAML
- 新流程：
  1. 从 DB 读取模板（完整的 Clash 配置骨架，但顶层 `proxies:` 字段为空或占位）
  2. 解析模板 YAML（`yaml.Unmarshal`）
  3. 遍历 `proxy-groups` 数组，查找包含 `'{{nodes}}'` 的 `proxies` 字段，替换为当前节点名数组
  4. 动态生成 `proxies:` 字段（遍历当前节点池，调用现有 `clashProxy` 函数生成每个节点的完整配置）
  5. 合并模板 + 动态节点配置 → 输出完整 YAML

**Placeholder Syntax**
- 仅支持 `{{nodes}}` 占位符（MVP，未来可扩展 `{{nodes_region_香港}}` 等）
- 占位符位置：`proxy-groups` 中某个组的 `proxies` 数组内，作为数组元素
- 展开规则：找到字符串值为 `'{{nodes}}'` 的元素，用当前所有节点名数组替换它

示例：
```yaml
# 模板（用户编辑）
proxy-groups:
  - name: ♻️ 手动切换
    type: select
    proxies:
      - DIRECT
      - '{{nodes}}'  # 占位符
  - name: 🧲 OpenAI
    type: select
    proxies: ['♻️ 手动切换', DIRECT]

# 生成后（假设聚合了3个节点）
proxy-groups:
  - name: ♻️ 手动切换
    type: select
    proxies:
      - DIRECT
      - 🇭🇰 香港 01
      - 🇯🇵 日本 02
      - 🇺🇸 美国 03
  - name: 🧲 OpenAI
    type: select
    proxies: ['♻️ 手动切换', DIRECT]
```

**Default Template Extraction**
- 从参考文件 `RwLftvSFh6in.yaml`（16173行）提取：
  - **保留**：`hosts` / `dns` / `proxy-groups`（41个应用组）/ `rules`（所有域名分流规则）
  - **修改**：将 `♻️ 手动切换` 组的 `proxies` 从337个硬编码节点名改为 `[DIRECT, '{{nodes}}']`
  - **删除**：顶层 `proxies:` 字段（337个节点的完整配置）- 这些由聚合系统动态生成
  - **保留**：基础配置字段（mixed-port/allow-lan/mode/log-level 等）

### Backend Modules

**internal/generator/template.go (新增)**
- `RenderTemplate(templateYAML string, nodes []*Node) ([]byte, error)` - 核心渲染函数
  - 解析模板 YAML
  - 查找并替换 `{{nodes}}` 占位符
  - 动态生成 `proxies:` 字段
  - 合并并输出完整 YAML
- `expandNodesPlaceholder(proxyGroups []map[string]any, nodeNames []string) error` - 递归遍历 proxy-groups，替换占位符
- `generateProxiesField(nodes []*Node) ([]map[string]any, error)` - 复用现有 `clashProxy` 函数

**internal/generator/default_template.yaml (新增)**
- 从参考文件提取的默认模板
- 使用 `//go:embed default_template.yaml` 打包
- `var DefaultTemplate string` 或 `//go:embed default_template.yaml; var defaultTemplateBytes []byte`

**internal/generator/clash.go (重构)**
- 保留原 `GenerateClash(nodes)` 签名以兼容旧调用（如果有外部依赖）
- 内部调用新的 `RenderTemplate`
- 或直接重构为从 store 读取模板

**internal/store/security.go (扩展)**
- 已有 `GetSetting(key) (string, error)`
- 已有 `SetSetting(key, value) error`
- 新增初始化逻辑：在 `Open()` 或首次访问 `clash_template` 时，如果不存在则写入默认模板

**internal/server/handlers.go (新增 API)**
- `GET /api/settings/template` - 返回当前模板（从 DB 读取）
- `PUT /api/settings/template` - 保存用户编辑的模板
  - 请求体：`{"template": "...yaml内容..."}`
  - 验证 YAML 格式（`yaml.Unmarshal`）
  - 验证占位符语法（可选，简单检查 `{{nodes}}` 拼写）
  - 保存到 DB（SetSetting）
- `POST /api/settings/template/reset` - 恢复默认模板
  - 从 embed 资源读取默认模板
  - 写入 DB（SetSetting）

**internal/server/server.go (修改)**
- `handleSubscription` 改为调用 `RenderTemplate`
  - 从 DB 读取模板（GetSetting）
  - 调用 `generator.RenderTemplate(template, nodes)`
  - 返回生成的 YAML

### Frontend Modules

**web/src/views/TemplateEditor.vue (新增)**
- Monaco Editor 集成（需安装 `monaco-editor` npm 包）
- 布局：编辑器占主体区域 + 顶部工具栏（保存/恢复默认/加载状态）
- 状态管理：loading（加载模板）/ saving（保存中）/ error（错误提示）
- 功能：
  - onMounted: `GET /api/settings/template` 加载当前模板
  - 保存按钮：`PUT /api/settings/template` 保存，成功后 toast 提示
  - 恢复默认按钮：确认对话框 → `POST /api/settings/template/reset` → 重新加载编辑器内容
  - 错误处理：YAML 格式错误/网络错误显示在编辑器下方

**web/src/router/index.ts (修改)**
- 新增路由 `/settings/template`，组件 `TemplateEditor.vue`

**web/src/views/Layout.vue 或 Settings.vue (修改)**
- 在设置菜单或导航栏添加"配置模板"入口

### Schema Changes

无需修改 schema，`settings` 表已存在且结构满足需求：
```sql
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

新增 key：`clash_template` (TEXT，存储完整的 YAML 模板，可能达到几十 KB)

### API Contracts

**GET /api/settings/template**
- Response 200: `{"template": "mixed-port: 7890\n..."}`
- Response 404: 模板未初始化（理论上不会出现，因为首次启动会初始化）

**PUT /api/settings/template**
- Request: `{"template": "...yaml..."}`
- Response 200: `{"success": true}`
- Response 400: YAML 格式错误，body 返回 `{"error": "invalid YAML: ..."}`

**POST /api/settings/template/reset**
- Response 200: `{"success": true}`

## Testing Decisions

### What Makes a Good Test
- 测试外部行为，不测试内部实现细节
- 优先测试业务逻辑边界（占位符替换/节点为空/格式错误）
- HTTP 测试使用 `httptest`，模拟完整请求-响应流程
- 数据层测试使用临时 DB（`t.TempDir()`）

### Modules to Test

**1. generator 包 - 单元测试**
- `TestRenderTemplate_Success` - 正常占位符替换
  - 输入：包含 `{{nodes}}` 的模板 + 3个节点
  - 验证：输出 YAML 的 `proxy-groups[0].proxies` 包含 DIRECT + 3个节点名
  - 验证：输出 YAML 的 `proxies` 字段包含3个节点的完整配置
- `TestRenderTemplate_MultiplePlaceholders` - 多个策略组都有占位符
  - 输入：两个组都有 `{{nodes}}`
  - 验证：两个组都被正确展开
- `TestRenderTemplate_NoPlaceholder` - 模板不含占位符
  - 输入：静态模板（没有 `{{nodes}}`）
  - 验证：原样返回（仅添加动态 proxies 字段）
- `TestRenderTemplate_EmptyNodes` - 节点池为空
  - 输入：包含 `{{nodes}}` 的模板 + 空节点数组
  - 验证：占位符被替换为空数组（或仅保留 DIRECT）
- `TestRenderTemplate_InvalidYAML` - 模板格式错误
  - 输入：语法错误的 YAML
  - 验证：返回错误
- `TestRenderTemplate_PreservesOtherFields` - 保留模板中的其他字段
  - 输入：包含 hosts/dns/rules 的模板
  - 验证：输出包含这些字段且内容未被修改

**Prior Art**: `internal/generator/generator_test.go` 的 `TestGenerateClash` 模式

**2. server 包 - HTTP 集成测试**
- `TestHandleSubscription_WithTemplate` - 端到端测试订阅生成
  - 准备：设置模板（包含 `{{nodes}}`）+ 添加3个节点
  - 请求：`GET /sub/{path}?token=xxx`
  - 验证：响应 200，body 是有效 YAML，包含 hosts/dns/proxy-groups/rules/proxies 所有字段
  - 验证：`proxy-groups` 中包含41个应用组（与默认模板一致）
  - 验证：`proxies` 字段包含3个节点的完整配置
- `TestTemplateAPI_GetAndSet` - 模板读写 API
  - `GET /api/settings/template` 返回默认模板
  - `PUT /api/settings/template` 修改模板
  - 再次 `GET` 验证修改已保存
- `TestTemplateAPI_Reset` - 恢复默认模板
  - 修改模板 → `POST /api/settings/template/reset` → `GET` 验证已恢复
- `TestTemplateAPI_InvalidYAML` - 保存无效模板
  - `PUT` 提交格式错误的 YAML → 验证返回 400 + 错误信息

**Prior Art**: `internal/server/server_test.go` 的 `newTestServer` + `httptest` 模式

**3. store 包 - 数据层测试**
- 已有 `TestGetSetting` / `TestSetSetting`，覆盖 settings 表 CRUD
- 无需新增测试（除非新增专门的模板初始化逻辑）

## Out of Scope

以下功能不在本 spec 范围内，未来可作为独立需求：

1. **高级占位符语法** - `{{nodes_region_香港}}` / `{{nodes_available}}` 等过滤占位符（MVP 仅支持 `{{nodes}}`）
2. **多模板支持** - 每个订阅地址选择不同模板（当前所有订阅地址共享一套模板）
3. **模板版本控制** - 保存历史版本/回滚（当前只有"恢复默认"）
4. **模板共享/导入** - 从 URL 导入第三方模板（当前只能手动复制粘贴）
5. **可视化规则编辑器** - 用表格/表单编辑 rules（当前是纯 YAML 文本编辑）
6. **模板变量系统** - 用户自定义变量（如 `{{MY_PROXY_PORT}}`）
7. **节点页管理功能** - 手动删除/启停节点（这是另一个独立需求）
8. **统计功能** - 订阅拉取统计/可用性统计（这是另一个独立需求）
9. **V2Ray 格式模板** - 当前只针对 Clash 格式，V2Ray 是简单的 base64 链接无需模板

## Further Notes

### 默认模板提取步骤（人工一次性操作）
1. 复制参考文件 `RwLftvSFh6in.yaml` 到 `internal/generator/default_template.yaml`
2. 删除顶层 `proxies:` 字段的所有内容（337个节点定义）
3. 找到 `♻️ 手动切换` 组，将其 `proxies` 数组改为 `[DIRECT, '{{nodes}}']`
4. 保留所有其他内容（hosts/dns/其余40个应用组/rules）
5. 验证 YAML 格式正确（可用在线工具或 `yq` 命令）

### Monaco Editor 集成注意事项
- 使用 CDN 或 npm 安装：`npm install monaco-editor`
- Vite 配置可能需要特殊处理（monaco-editor 的 worker 文件）
- 推荐使用 `@guolao/vue-monaco-editor` 封装（Vue 3 友好）
- 设置语言模式为 `yaml`，启用基础语法检查

### 性能考虑
- 默认模板大小约 16KB（从16k行压缩到几千行，去掉337个节点定义）
- 存入 SQLite TEXT 字段无压力（TEXT 类型最大 2GB）
- 订阅生成时每次从 DB 读取模板 + 解析 YAML，延迟增加约 5-10ms（可接受）
- 未来优化：可在内存缓存模板（监听 SetSetting 事件失效缓存）

### 兼容性
- 保留原 `GenerateClash(nodes)` 函数签名，内部改为调用 `RenderTemplate`
- 如果外部有直接调用 `GenerateClash`，行为保持一致（生成完整配置，只是内容更丰富）
- V2Ray 格式不受影响（`GenerateV2Ray` 保持原样）

### 词汇表补充（需写入 CONTEXT.md）
- **配置模板 (Template)**: Clash 配置的骨架，包含 hosts/dns/proxy-groups/rules，通过占位符动态注入节点名。
- **占位符 (Placeholder)**: 模板中的特殊标记（如 `{{nodes}}`），订阅生成时被实际节点名替换。

### ADR 记录建议
考虑记录以下决策为 ADR（如果满足"难以逆转、未来会疑惑、有权衡"三条件）：
- **ADR 0006: 配置模板采用单一全局模板而非多模板** - 简化维护，所有订阅地址共享一套规则
- **ADR 0007: 占位符语法使用 Mustache 风格 `{{...}}` 而非自定义语法** - 开发者熟悉度高，可读性强
