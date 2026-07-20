# 地区识别与分组系统规格

## 目标

实现多机场节点的地区识别、按地区筛选与分组，解决节点命名不一致问题。

## 核心原则

1. **节点原名不变**：`nodes` 表永远保留机场返回的原始名称
2. **全部入池**：聚合时所有拉到的节点都进池（包括未识别地区）
3. **订阅时筛选**：地区白名单和分组在订阅生成时生效
4. **模板可编辑**：平台提供默认地区分组模板，用户可修改

## 功能分解

### 1. 地区识别（Region Recognition）

#### 数据库

```sql
-- 地区识别规则表
CREATE TABLE region_rules (
  id INTEGER PRIMARY KEY,
  region_code TEXT NOT NULL,      -- 标准地区代码: HK, SG, US, JP, ...
  region_name TEXT NOT NULL,      -- 地区显示名: 香港, 新加坡, 美国, ...
  pattern TEXT NOT NULL,          -- 匹配模式: "香港", "HK", "🇭🇰"
  priority INTEGER NOT NULL DEFAULT 0,  -- 优先级（越大越优先）
  UNIQUE(region_code, pattern)
);

-- 优先级规则：
--   100: 国旗 emoji（最明确）
--   80:  中文全名
--   60:  英文全名
--   40:  常用缩写
--   20:  其他别名
```

#### 预置规则（启动时初始化）

常见地区（参考 subconverter）：
- **香港 (HK)**: 🇭🇰(100), 香港(80), Hong Kong(60), HK(40), 港(40)
- **新加坡 (SG)**: 🇸🇬(100), 新加坡(80), Singapore(60), SG(40), 狮城(40), 新(20)
- **美国 (US)**: 🇺🇸(100), 美国(80), United States(60), US(40), USA(40), 美(20)
- **日本 (JP)**: 🇯🇵(100), 日本(80), Japan(60), JP(40), 日(20)
- **台湾 (TW)**: 🇹🇼(100), 台湾(80), Taiwan(60), TW(40), 台(20)
- **韩国 (KR)**: 🇰🇷(100), 韩国(80), Korea(60), KR(40), 韩(20)
- **英国 (GB)**: 🇬🇧(100), 英国(80), United Kingdom(60), UK(40), GB(40), 英(20)
- **德国 (DE)**: 🇩🇪(100), 德国(80), Germany(60), DE(40), 德(20)
- **法国 (FR)**: 🇫🇷(100), 法国(80), France(60), FR(40), 法(20)
- **加拿大 (CA)**: 🇨🇦(100), 加拿大(80), Canada(60), CA(40), 加(20)
- **澳大利亚 (AU)**: 🇦🇺(100), 澳大利亚(80), 澳洲(80), Australia(60), AU(40), 澳(20)

#### 识别逻辑

```go
// RecognizeRegion 从节点名提取地区代码
// 按优先级倒序匹配规则，返回第一个命中的 region_code
// 子串匹配，大小写不敏感
// 未匹配返回 "Unknown"
func RecognizeRegion(nodeName string, rules []RegionRule) string
```

#### 集成点

在 `Aggregator.Run` 的聚合流水线中，拉取节点后、入池前调用识别器更新 `nodes.region`：

```go
// aggregator.go
for _, node := range fetchedNodes {
    node.Region = recognizer.RecognizeRegion(node.Name)
    store.UpsertNode(node)  // region 字段一并写入
}
```

### 2. 地区白名单（Region Whitelist）

#### 配置存储

```sql
-- settings 表新增条目
INSERT INTO settings (key, value) VALUES 
  ('region_whitelist', '[]');  -- JSON 数组: ["HK","SG","US"] 或 [] (空=全部下发)
```

#### 后台 API

```
GET  /api/settings/region-whitelist   → {whitelist: ["HK","SG"]}
POST /api/settings/region-whitelist   ← {whitelist: ["HK","SG","US"]}
```

#### 前端 UI

设置页（Settings.vue）新增"地区白名单"模块：
- 显示所有可用地区（从 `region_rules` 的 `region_code` 去重）
- 多选框选择白名单地区
- 空=全部下发；非空=仅下发选中地区
- 实时统计：`当前节点池: HK×50, SG×30, US×20, Unknown×5 | 白名单生效后将下发: 100 个节点`

#### 订阅生成时筛选

```go
// generator/template.go buildProxies() 增加过滤
func buildProxies(nodes []*subscription.Node, whitelist []string) (...)
    for _, node := range nodes {
        // 白名单筛选
        if len(whitelist) > 0 && !contains(whitelist, node.Region) {
            continue  // 不在白名单，跳过
        }
        // ... 原有逻辑
    }
```

### 3. 分区占位符（Region Placeholder）

#### 语法

- `{{nodes}}` — 注入全部节点（受地区白名单约束）
- `{{nodes:region=HK}}` — 仅注入香港节点
- `{{nodes:region=HK,SG,US}}` — 注入多个地区节点（逗号分隔）

#### 实现

```go
// generator/template.go
const (
    nodesPlaceholder       = "{{nodes}}"
    nodesRegionPlaceholder = "{{nodes:region="  // 前缀匹配
)

// expandNodesPlaceholder 增强
func expandNodesPlaceholder(cfg map[string]any, allNodes []string, nodesByRegion map[string][]string) {
    for _, group := range groups {
        for _, item := range group["proxies"] {
            if item == "{{nodes}}" {
                // 注入全部节点
                expand(allNodes)
            } else if strings.HasPrefix(item, "{{nodes:region=") {
                // 解析 region=HK,SG
                regions := parseRegions(item)
                expand(filterByRegions(allNodes, regions, nodesByRegion))
            }
        }
    }
}
```

#### 默认模板更新

```yaml
proxy-groups:
  - name: 🚀 节点选择
    type: select
    proxies:
      - ♻️ 自动选择
      - 🇭🇰 香港节点
      - 🇸🇬 新加坡节点
      - 🇺🇸 美国节点
      - 🇯🇵 日本节点
      - {{nodes}}  # 全部节点手动选择

  - name: ♻️ 自动选择
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - {{nodes}}  # 全部节点参与测速

  - name: 🇭🇰 香港节点
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - {{nodes:region=HK}}

  - name: 🇸🇬 新加坡节点
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - {{nodes:region=SG}}

  - name: 🇺🇸 美国节点
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - {{nodes:region=US}}

  - name: 🇯🇵 日本节点
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies:
      - {{nodes:region=JP}}
```

### 4. 边界场景

#### 4.1 地区识别失败
- `nodes.region = "Unknown"`
- 若白名单非空且不含 `Unknown`，这些节点不下发
- 前端显示"未识别地区"统计，提示用户检查

#### 4.2 分区占位符为空
- `{{nodes:region=JP}}` 但无日本节点 → `proxies: []`（空组）
- 不删除组，不报错（保持模板编辑的灵活性）

#### 4.3 名称去重
- 当前逻辑：`uniqueName()` 后缀递增（`"节点"` → `"节点 2"`, `"节点 3"`）
- 保持不变（节点名映射留待后续实现）

## 实现顺序

1. **地区识别**（store + aggregator）
   - [ ] 创建 `region_rules` 表 + 迁移
   - [ ] 预置规则初始化（`internal/store/region.go`）
   - [ ] 实现 `RecognizeRegion` 识别器
   - [ ] 集成到聚合流水线
   - [ ] 测试：拉取后 `nodes.region` 正确填充

2. **地区白名单**（settings + generator）
   - [ ] `region_whitelist` 设置项
   - [ ] API: GET/POST `/api/settings/region-whitelist`
   - [ ] 前端：Settings.vue 地区选择器
   - [ ] `buildProxies` 增加白名单过滤
   - [ ] 测试：白名单 `[HK]` 时仅下发香港节点

3. **分区占位符**（generator + template）
   - [ ] 解析 `{{nodes:region=XX}}` 语法
   - [ ] `expandNodesPlaceholder` 按地区过滤注入
   - [ ] 更新默认模板（添加地区分组）
   - [ ] 测试：`{{nodes:region=HK}}` 仅注入香港节点

4. **前端显示优化**（可选）
   - [ ] 节点列表按地区分组显示
   - [ ] 实时统计各地区节点数
   - [ ] 白名单配置时显示影响预览

## 测试场景

1. **基础识别**：拉取含"XX 香港""HK-01""🇭🇰节点"的机场 → 全部识别为 HK
2. **白名单筛选**：配 `[HK,SG]` → 订阅只含香港+新加坡节点
3. **分区占位符**：模板含 `{{nodes:region=US}}` → 该组只有美国节点
4. **未识别地区**：节点名"神秘节点" → region=Unknown，白名单 `[HK]` 时不下发
5. **空组处理**：白名单 `[HK]` 但模板有 `{{nodes:region=JP}}` → 日本组为空数组

## 不做的事（留待后续）

- 节点名映射/重命名（当前保持机场原名）
- 地区规则热更新（规则固化在代码，有需要再改成可配置）
- 多地区节点智能识别（"香港中转新加坡" → 取第一个匹配 HK）
