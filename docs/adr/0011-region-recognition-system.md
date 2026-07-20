# ADR 0011: 地区识别与分组系统

## 状态
**已采纳** — 2026-07

## 背景

用户订阅 N 个机场，每个机场的节点命名不一致：
- 机场A: `"XX 香港"`, `"HK-Premium-01"`, `"🇭🇰 高速节点"`
- 机场B: `"Hong Kong Elite"`, `"HK Server 1"`

终端用户拿到的订阅里，节点名五花八门，proxy-group 也无法按地区自动分组（手动枚举节点名不现实）。

**核心需求**：
1. 聚合时自动识别节点地区（从节点名提取地区代码）
2. 订阅生成时按地区筛选（地区白名单）
3. 模板支持按地区注入节点（分区占位符 `{{nodes:region=HK}}`）
4. 节点原名保持不变（便于追溯，映射在订阅生成时做）

## 决策

### 1. 地区识别（Region Recognition）

**数据模型**：
```sql
CREATE TABLE region_rules (
  id INTEGER PRIMARY KEY,
  region_code TEXT NOT NULL,      -- HK, SG, US, ...
  region_name TEXT NOT NULL,      -- 香港, 新加坡, 美国, ...
  pattern TEXT NOT NULL,          -- "香港", "🇭🇰", "HK", ...
  priority INTEGER NOT NULL DEFAULT 0  -- 优先级（100=emoji, 80=中文, 60=英文, 40=缩写）
);
```

**预置规则**（启动时初始化）：
- 11 个常见地区（HK/SG/US/JP/TW/KR/GB/DE/FR/CA/AU）
- 每个地区多模式匹配（国旗 emoji + 中文全名 + 英文全名 + 缩写）
- 按优先级倒序匹配，取第一个命中的地区代码

**集成点**：
聚合流水线在拉取后、健康检查前调用识别器，填充 `nodes.region` 字段。未匹配的节点 `region = "Unknown"`。

### 2. 地区白名单（Region Whitelist）

**配置存储**：
```json
// settings.region_whitelist
["HK", "SG", "US"]  // JSON 数组，空 = 全部下发
```

**过滤规则**：
- 订阅生成时生效（节点仍全部入池，只是部分地区不下发给终端）
- 自建节点豁免（不受白名单约束，永远下发）
- 空白名单 = 全部地区通过（兼容旧行为）

**过滤顺序**：
订阅时三道过滤依次执行：**地区白名单** → 关键词白名单 → 关键词黑名单 → 机场屏蔽。地区白名单优先（精确、高效），关键词白名单作为补充（字符串匹配、粗粒度）。

### 3. 分区占位符（Region Placeholder）

**语法**：
- `{{nodes}}` — 注入全部节点（受地区白名单约束）
- `{{nodes:region=HK}}` — 仅注入香港节点
- `{{nodes:region=HK,SG,US}}` — 注入多地区（逗号分隔）

**默认模板**：
```yaml
proxy-groups:
  - name: 🚀 节点选择
    proxies: ['{{nodes}}']  # 全部节点手动选择
  
  - name: ♻️ 自动选择
    type: url-test
    proxies: ['{{nodes}}']  # 全部节点参与测速
  
  - name: 🇭🇰 香港节点
    type: url-test
    proxies: ['{{nodes:region=HK}}']  # 只有香港节点
```

**边界场景**：
- 某地区无节点时，该组 `proxies: []`（空数组），不删组、不报错
- 同组内重复占位符只展开第一次

### 4. 节点名映射（留待后续）

当前保持机场原名，订阅生成时不重命名。未来可扩展：
- 订阅时可选的节点名映射规则（如 `"香港-{序号}"`）
- `nodes` 表永远保留原名（便于追溯）
- 映射仅影响下发给终端的订阅内容

## 实现细节

### 地区识别器（RegionRecognizer）

```go
// store/region.go
type RegionRecognizer struct {
    rules []RegionRule  // 按优先级+长度倒序
}

func (r *RegionRecognizer) Recognize(nodeName string) string {
    lowerName := strings.ToLower(nodeName)
    for _, rule := range r.rules {
        if strings.Contains(lowerName, strings.ToLower(rule.Pattern)) {
            return rule.RegionCode
        }
    }
    return "Unknown"
}
```

**匹配策略**：
- 子串匹配（`strings.Contains`）
- 大小写不敏感
- 优先级：100(emoji) > 80(中文) > 60(英文) > 40(缩写) > 20(别名)
- 同优先级时按 pattern 长度倒序（优先匹配长模式）

### 分区占位符解析

```go
// generator/template.go
func expandList(items []any, allNames []string, nodesByRegion map[string][]string) []any {
    for _, item := range items {
        if s, ok := item.(string); ok {
            if s == "{{nodes}}" {
                // 注入全部节点
            } else if strings.HasPrefix(s, "{{nodes:region=") {
                // 解析地区代码（如 "HK,SG,US"）
                // 注入对应地区节点
            }
        }
    }
}
```

**nodesByRegion 构建**：
`RenderTemplate` 在生成 proxies 后，按 `node.Region` 分组节点名（已去重），传递给占位符展开器。

## 权衡

### 为什么在订阅时筛选，而非聚合时？

- **灵活性**：节点池保留全部地区，用户可随时调整白名单，无需重新拉取
- **调试性**：后台可看到全部节点，便于排查"为什么某节点没下发"
- **幂等性**：白名单配置不影响聚合结果，订阅生成是纯函数

### 为什么用子串匹配而非正则？

- **性能**：子串匹配 O(n)，正则编译+执行慢
- **简单**：规则库易维护，不易出错
- **够用**：覆盖 95%+ 机场节点命名（emoji/中文/英文/缩写已全）

### 为什么不支持复杂地区规则（如排除/优先级/别名）？

**当前够用** — 11 个常见地区 × 5 模式/地区 = 55 条规则已覆盖绝大多数场景。需要时再扩展（可配置规则表、用户自定义规则等）。

## 后果

### 正面

- **统一体验**：终端用户拿到的订阅，节点按地区清晰分组
- **精确控制**：地区白名单按地区代码过滤，比关键词白名单更精确
- **易扩展**：规则库易扩展（新增地区/模式），不影响现有节点

### 负面

- **未识别节点**：冷门地区（如以色列、埃及）会被标记 `Unknown`，需手动补充规则
- **多地区节点**：`"香港中转新加坡"` 只取第一个匹配（HK），无法标记为中转节点
- **模板兼容性**：旧模板（含大量应用分流组）未使用分区占位符，需手动迁移

## 测试

### 地区识别测试

```go
{"🇭🇰 香港节点", "HK"},
{"XX 香港", "HK"},
{"HK-Premium-01", "HK"},
{"Hong Kong Server", "HK"},
{"新加坡高速", "SG"},
{"US Server 01", "US"},
{"神秘节点X", "Unknown"},
```

### 分区占位符测试

```yaml
# 输入：HK×2, SG×1, US×1, Unknown×1
proxy-groups:
  - name: 🇭🇰 香港节点
    proxies: ['{{nodes:region=HK}}']  # 展开为 2 个香港节点
  - name: 🌏 亚洲节点
    proxies: ['{{nodes:region=HK,SG}}']  # 展开为 3 个节点（HK+SG）
  - name: 🇯🇵 日本节点
    proxies: ['{{nodes:region=JP}}']  # 空数组（无日本节点）
```

### 白名单过滤测试

- 白名单 `["HK", "SG"]` + 节点池 12 个（HK×2, SG×1, US×3, Unknown×6）
- 订阅生成后仅下发 3 个节点（HK×2 + SG×1）
- 自建节点（source=self_hosted）仍下发（豁免）

## 参考

- subconverter 地区正则库（国旗 emoji 映射）
- Clash 文档：proxy-groups 语法
