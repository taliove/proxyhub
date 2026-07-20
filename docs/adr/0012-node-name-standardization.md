# ADR 0012: 节点名称标准化系统

## 状态
**已采纳** — 2026-07

## 背景

多个机场的节点命名风格不一致:
- 机场A: `"HK-01 | 香港IEPL专线"`
- 机场B: `"🇭🇰 Hong Kong 01"`
- 机场C: `"香港高速节点-1"`

用户在终端客户端看到的节点列表混乱,难以快速识别:
- 同地区节点名格式各异
- 无法快速区分来自哪个机场
- 节点顺序不稳定(名称不统一导致排序混乱)

**核心需求**:
1. 统一节点命名格式: 地区国旗 + 地区名 + 机场标识 + 序号
2. 保留原始节点名(便于调试和追溯)
3. 支持配置化(不同订阅端点可使用不同格式)
4. 机场简称自动生成且可手动覆盖

## 决策

### 1. 双名称存储策略

**数据模型**:
```go
type Node struct {
    Name        string  // 原始节点名(来自机场,永不修改)
    DisplayName string  // 标准化名称(订阅生成时计算并缓存)
    Region      string  // 地区代码(HK/SG/US...)
    Source      string  // 机场名称
    NodeKey     string  // 唯一标识(server:port 或 server:port:sni)
    // ...其他字段
}
```

**转换时机**: 订阅生成时动态计算标准化名称
- 优点: 原始名称可追溯,格式可随时调整
- 缺点: 订阅生成时需要额外计算

**display_name 缓存**: 可选的性能优化
- 首次生成订阅时计算并缓存到 `display_name` 字段
- 格式配置变更时清空缓存,重新计算
- 减少重复计算开销

### 2. 机场简称生成规则

**自动生成策略**:
```
中文机场名: 取拼音首字母大写
  "极速机场" → "JS"
  "悠悠云" → "YY"

英文机场名: 取前2-3个大写字母
  "FlowerCloud" → "FC"
  "BestProxy" → "BP"

去重处理: 遇到重复加数字后缀
  "极速机场" → "JS"
  "极速专线" → "JS2"
  "极速VPN" → "JS3"
```

**存储与覆盖**:
```sql
-- airports 表增加字段
ALTER TABLE airports ADD COLUMN abbr TEXT;  -- 机场简称(NULL时自动生成)
```

管理员可在"机场管理"页面手动修改简称:
- 默认显示自动生成的简称
- 点击编辑可自定义(如 "JS" → "JiSu")
- 手动设置的简称持久化到 `abbr` 字段,优先使用

### 3. 节点序号生成规则

**排序依据**: 按 NodeKey 字典序排序
```
节点A: server=1.2.3.4:443 → NodeKey=1.2.3.4:443 → 序号 01
节点B: server=5.6.7.8:443 → NodeKey=5.6.7.8:443 → 序号 02
```

**为什么不按延迟排序**:
- ❌ 延迟会波动,导致节点序号频繁变化
- ❌ 用户订阅更新后节点顺序变化,影响体验
- ✅ NodeKey 稳定,同一节点的序号不会变
- ✅ 用户可以记住常用节点的序号

**序号格式**: 两位数补零 `01, 02, ..., 10, 11`
- 同机场同地区内独立编号
- 不同机场的序号互不影响

### 4. 名称模板系统

**模板语法**: 使用 `{变量名}` 占位符

**可用变量**:
- `{emoji}` - 地区国旗emoji (🇭🇰)
- `{region}` - 地区中文名 (香港)
- `{region_code}` - 地区代码 (HK)
- `{source}` - 机场全名 (极速机场)
- `{source_abbr}` - 机场简称 (JS)
- `{index}` - 节点序号 (01, 02...)
- `{original_name}` - 原始节点名

**默认模板**:
```
{emoji} {region} {source_abbr}-{index}
```

生成结果示例:
```
🇭🇰 香港 JS-01
🇭🇰 香港 JS-02
🇸🇬 新加坡 FC-01
```

**配置位置**: 系统设置(`settings` 表,后台「订阅设置」热改)

> **实现说明(与初稿偏差)**:初稿设想按订阅端点在 `config.yaml` 配置。但本项目订阅端点是
> DB 驱动(`endpoints` 表:alias/path/token),格式由请求 `?format=` 运行时决定,一个端点可
> 同时出 Clash 与 V2Ray。故把标准化配置放进 `settings` 表更贴合既有架构(与 `region_whitelist`、
> `filter_keywords` 等订阅期配置同源),免重启即时生效。采用**全局配置**:

```
settings.standardize_names = "true" | "false"   # 全局开关,默认 false(兼容旧行为)
settings.name_template     = "{emoji} {region} {source_abbr}-{index}"  # 空则用默认模板
```

**配置灵活性**:
- 后台「系统设置 → 订阅设置」热改,无需重启
- 可随时调整模板,不需要重新刷新节点
- 禁用标准化时使用原始节点名(兼容旧行为)
- 序号固定两位补零(`%02d`),暂不单独暴露 `index_format`

**按端点覆盖(已实现)**:全局配置是默认值,`endpoints` 表另有两列覆盖:

```
endpoints.name_mode     = "" | "on" | "off"   # 空=跟随全局开关,on/off=强制
endpoints.name_template = "..."                # 空=用全局模板,非空=该端点专用模板
```

生效配置解析(`server.resolveNameConfig`):以全局为基准,端点 `name_mode` 覆盖开关、
`name_template` 覆盖模板。这样 `/sub` 的每个订阅地址可独立选择是否标准化、用什么格式
(如给老人机端点关标准化保留原名,给主力端点开 emoji 模板),而无端点上下文的管理列表
沿用全局。后台「订阅地址管理 → 命名设置」逐端点配置。

## 实现细节

### 机场简称生成器

```go
// internal/subscription/abbreviation.go
package subscription

import (
    "github.com/mozillazg/go-pinyin"
)

// GenerateAbbreviation 生成机场简称
func GenerateAbbreviation(airportName string) string {
    // 中文:取拼音首字母
    if isChinese(airportName) {
        pinyinArgs := pinyin.NewArgs()
        pinyinArgs.Style = pinyin.FirstLetter
        result := pinyin.Pinyin(airportName, pinyinArgs)
        return strings.ToUpper(strings.Join(result, ""))
    }
    
    // 英文:取前2-3个大写字母
    return extractEnglishPrefix(airportName, 2, 3)
}

// DeduplicateAbbreviations 去重:遇到重复加数字后缀
func DeduplicateAbbreviations(abbrs map[string]string) map[string]string {
    // abbrs: airportName -> auto-generated abbr
    // 返回: airportName -> deduplicated abbr
}
```

### 节点名称标准化器

```go
// internal/subscription/standardizer.go
package subscription

type Standardizer struct {
    template   string           // 名称模板
    airportAbbrs map[string]string  // 机场简称映射
}

// Standardize 标准化节点名称
func (s *Standardizer) Standardize(node *Node, index int) string {
    template := s.template
    
    // 替换变量
    template = strings.ReplaceAll(template, "{emoji}", getEmoji(node.Region))
    template = strings.ReplaceAll(template, "{region}", getRegionName(node.Region))
    template = strings.ReplaceAll(template, "{region_code}", node.Region)
    template = strings.ReplaceAll(template, "{source}", node.Source)
    template = strings.ReplaceAll(template, "{source_abbr}", s.airportAbbrs[node.Source])
    template = strings.ReplaceAll(template, "{index}", fmt.Sprintf("%02d", index))
    template = strings.ReplaceAll(template, "{original_name}", node.Name)
    
    return template
}
```

### 订阅生成集成

```go
// internal/server/template.go (修改)
func (s *Server) RenderTemplate(endpoint *config.Endpoint) (string, error) {
    nodes := s.aggregator.GetNodes()  // 获取全部节点
    
    // 按订阅端点配置决定是否标准化
    if endpoint.StandardizeNames {
        standardizer := NewStandardizer(
            endpoint.NameTemplate,
            s.getAirportAbbreviations(),
        )
        
        // 按机场+地区分组,生成序号
        groupedNodes := groupBySourceAndRegion(nodes)
        for source, regionMap := range groupedNodes {
            for region, regionNodes := range regionMap {
                // 按 NodeKey 排序
                sort.Slice(regionNodes, func(i, j int) bool {
                    return regionNodes[i].NodeKey() < regionNodes[j].NodeKey()
                })
                
                // 生成标准化名称
                for idx, node := range regionNodes {
                    node.DisplayName = standardizer.Standardize(node, idx+1)
                }
            }
        }
    } else {
        // 不标准化:使用原始名称
        for _, node := range nodes {
            node.DisplayName = node.Name
        }
    }
    
    // 后续流程使用 DisplayName 生成订阅
    // ...
}
```

## 权衡

### 为什么在订阅生成时转换,而非刷新时?

**优点**:
- 原始名称可追溯(调试时看到机场原名)
- 格式可随时调整(修改配置后立即生效,无需重新刷新)
- 节点管理页面可同时显示原始名+标准名(便于对比)

**缺点**:
- 订阅生成时需要额外计算(可通过 display_name 缓存缓解)
- 逻辑稍复杂(需要维护模板解析器)

### 为什么按 NodeKey 排序而非延迟?

**延迟排序的问题**:
- 延迟会波动,每次刷新后序号可能变化
- 用户订阅更新后节点顺序变化,记忆失效
- 低延迟节点可能不稳定,排前面反而误导用户

**NodeKey 排序的优势**:
- 稳定性:同一节点的序号永不变化
- 可预测性:用户可以记住常用节点(如 "JS-03")
- 调试友好:序号与节点物理地址关联,便于排查

**性能优选**: 由客户端负责(url-test / fallback 组)
- ProxyHub 提供稳定的节点列表
- 客户端根据实时延迟选择最优节点
- 职责分离:聚合器负责可用性,客户端负责优选

### 为什么支持多种模板而非固定格式?

**不同用户的偏好不同**:
- 简洁派: `"HK-JS-01"` (省去emoji和中文)
- 详细派: `"🇭🇰 香港 [极速机场] 01"` (保留机场全名)
- 原教旨派: 保持机场原名不做任何转换

**不同客户端的兼容性不同**:
- Clash: 支持emoji和中文
- V2Ray: 某些版本不支持emoji
- Quantumult X: 节点名长度限制

通过模板系统,一套后端可满足所有需求,无需为每种客户端维护单独的代码分支。

## 后果

### 正面

- **用户体验提升**: 节点列表清晰有序,快速识别地区和机场
- **调试能力增强**: 保留原始名称,便于追溯问题节点
- **配置灵活**: 不同订阅端点可使用不同格式
- **维护成本低**: 机场简称自动生成,管理员只需覆盖少数特例

### 负面

- **复杂度增加**: 模板解析器增加代码量
- **UI改造成本**: 节点管理页面需要展示原始名+标准名
- **机场管理页改造**: 需要增加简称编辑功能
- **配置学习成本**: 管理员需要理解模板语法

### 迁移成本

- **已有订阅**: 默认关闭标准化(standardize_names: false),保持兼容
- **新部署**: 默认启用标准化,提供最佳体验
- **渐进迁移**: 用户可逐个订阅端点启用标准化,测试无误后全量开启

## 测试场景

### 机场简称生成

```go
// 中文机场
"极速机场" → "JS"
"悠悠云" → "YY"  
"飞跃VPN" → "FY"

// 英文机场
"FlowerCloud" → "FC"
"BestProxy" → "BP"
"CloudVPN" → "CV"

// 去重
机场列表: ["极速机场", "极速专线", "极速VPN"]
生成结果: ["JS", "JS2", "JS3"]
```

### 节点名称标准化

```
输入:
  - 原始名: "HK-01 | 香港IEPL专线"
  - 地区: HK
  - 机场: 极速机场(简称 JS)
  - 序号: 1
  - 模板: "{emoji} {region} {source_abbr}-{index}"

输出:
  - 标准名: "🇭🇰 香港 JS-01"
```

### 序号稳定性

```
场景: 机场"极速"有3个香港节点
刷新前: NodeKey排序 → ["1.2.3.4:443", "5.6.7.8:443", "9.10.11.12:443"] → 序号 [01, 02, 03]
刷新后: 延迟变化,但NodeKey不变 → 序号仍为 [01, 02, 03]
```

### 多订阅端点配置

```yaml
# Clash订阅: 标准化格式
endpoints:
  - path: /sub/clash
    standardize_names: true
    name_template: "{emoji} {region} {source_abbr}-{index}"
    # 生成: "🇭🇰 香港 JS-01"

# V2Ray订阅: 简洁格式(无emoji)
  - path: /sub/v2ray
    standardize_names: true
    name_template: "{region} {source_abbr}-{index}"
    # 生成: "香港 JS-01"

# 原始订阅: 保持机场原名
  - path: /sub/raw
    standardize_names: false
    # 生成: "HK-01 | 香港IEPL专线"
```

## 参考

- Clash 节点命名最佳实践
- go-pinyin: https://github.com/mozillazg/go-pinyin
- subconverter 地区emoji映射
