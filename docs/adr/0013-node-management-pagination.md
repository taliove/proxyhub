# ADR 0013: 节点管理分页与筛选系统

## 状态
**已采纳** — 2026-07

## 背景

当前节点管理页面(`/nodes`)存在的问题:
1. **性能问题**: 无分页,一次性加载全部节点(可能上千个),页面卡顿
2. **筛选功能弱**: 只有"按机场屏蔽"下拉框,无法按地区/类型/状态筛选
3. **UI布局混乱**: 筛选和批量操作混在一起,操作逻辑不清晰
4. **列信息不足**: 缺少"原始名称"和"标准名称"对比展示

**核心需求**:
1. 后端分页,支持大量节点场景(1000+ 节点)
2. 多维度筛选(地区/类型/状态/机场)和排序
3. UI重构为三段式布局(筛选区/批量操作区/表格区)
4. 展示原始名称和标准化名称

## 决策

### 1. 后端分页API设计

**分页参数**: 采用页码模式(而非游标模式)
```
GET /api/nodes?page=1&page_size=20&...filters&...sorts
```

**为什么用页码模式**:
- 用户可以直接跳转到任意页(快速定位)
- Element Plus 分页组件默认就是页码模式
- 节点数据变化不频繁(刷新是定时任务),数据一致性问题不严重

### 2. 筛选与排序维度

**筛选字段**:
- `region` - 地区代码(HK/SG/US...)
- `type` - 节点类型(vmess/vless/trojan/ss/hysteria2)
- `available` - 可用状态(true/false)
- `blocked` - 屏蔽状态(true/false)
- `source` - 机场名称(支持模糊搜索)

**排序字段**:
- `latency` - 延迟(默认升序)
- `name` - 节点名称
- `region` - 地区
- `source` - 机场

**API示例**:
```
GET /api/nodes?page=1&page_size=20
  &region=HK           // 筛选:只看香港节点
  &type=vmess          // 筛选:只看vmess类型
  &available=true      // 筛选:只看可用节点
  &blocked=false       // 筛选:排除已屏蔽节点
  &source=极速          // 筛选:机场名包含"极速"
  &sort_by=latency     // 排序:按延迟
  &sort_order=asc      // 排序:升序
```

**响应格式**:
```json
{
  "nodes": [...],       // 当前页节点列表
  "total": 1234,       // 总节点数
  "page": 1,           // 当前页码
  "page_size": 20,     // 每页大小
  "total_pages": 62    // 总页数
}
```

### 3. UI三段式布局

```
┌─────────────────────────────────────────────────────┐
│ 节点管理                                              │
├─────────────────────────────────────────────────────┤
│ 【筛选区】                                            │
│ [地区▼] [类型▼] [状态▼] [屏蔽▼] [机场搜索] [重置]    │
├─────────────────────────────────────────────────────┤
│ 【批量操作区】                                         │
│ 已选 3 项  [屏蔽选中] [取消屏蔽]  | [按机场屏蔽▼]     │
├─────────────────────────────────────────────────────┤
│ 【表格区】                                            │
│ ☑ 原始名称    标准名称   类型 地区 延迟 状态 来源 操作 │
│ ☑ HK-01...   🇭🇰香港JS-01 vmes HK  50ms ✓  极速 [取消]│
│ ...                                                   │
├─────────────────────────────────────────────────────┤
│ 【分页区】                                            │
│ 共 1234 条  [<] 1 2 3 4 5 [>]  每页 [20▼] 条        │
└─────────────────────────────────────────────────────┘
```

**关键改进**:
- 筛选区和批量操作区分离,逻辑清晰
- 新增"原始名称"和"标准名称"两列(便于对比)
- 分页组件显示总数和跳页功能
- 每页条数可配置(10/20/50/100)

### 4. 地区映射表补全

当前 `region_rules` 表只有11个地区,需要补全常见地区。

**新增地区**:
- 🇮🇳 印度 (India/IN)
- 🇷🇺 俄罗斯 (Russia/RU)
- 🇳🇱 荷兰 (Netherlands/NL)
- 🇹🇷 土耳其 (Turkey/TR)
- 🇵🇭 菲律宾 (Philippines/PH)
- 🇹🇭 泰国 (Thailand/TH)
- 🇦🇷 阿根廷 (Argentina/AR)

**映射规则**(每个地区):
```sql
-- 以印度为例
INSERT INTO region_rules (region_code, region_name, pattern, priority) VALUES
  ('IN', '印度', '🇮🇳', 100),  -- 国旗emoji
  ('IN', '印度', '印度', 80),   -- 中文全名
  ('IN', '印度', 'India', 60), -- 英文全名
  ('IN', '印度', 'IN', 40);    -- 缩写
```

## 实现细节

> **实现说明(与初稿偏差)**:初稿示例用 SQL 查询 `nodes` 表分页。实际实现改为在**内存节点池**上
> 做筛选→排序→分页(`internal/server/nodequery.go` 的 `QueryNodes`)。原因:订阅生成与后台列表都
> 读同一份内存池(含运行时注入的自建节点、反映最近一次刷新),DB 的 `nodes` 表只是重启回填快照。
> 在内存池上查询保证后台「所见即所得」,与订阅同源;节点规模(千级)下内存过滤开销可忽略。
> 下方 SQL 示例保留作参考。

### 后端分页查询

```go
// internal/store/nodes.go
type NodeQueryParams struct {
    Page     int
    PageSize int
    
    // 筛选
    Region    string
    Type      string
    Available *bool  // 指针类型,nil表示不筛选
    Blocked   *bool
    Source    string  // 模糊匹配
    
    // 排序
    SortBy    string  // latency/name/region/source
    SortOrder string  // asc/desc
}

type NodeQueryResult struct {
    Nodes      []*subscription.Node
    Total      int
    Page       int
    PageSize   int
    TotalPages int
}

func (s *Store) QueryNodes(params NodeQueryParams) (*NodeQueryResult, error) {
    // 构建WHERE子句
    where := []string{"1=1"}
    args := []interface{}{}
    
    if params.Region != "" {
        where = append(where, "region = ?")
        args = append(args, params.Region)
    }
    if params.Type != "" {
        where = append(where, "type = ?")
        args = append(args, params.Type)
    }
    if params.Available != nil {
        where = append(where, "available = ?")
        args = append(args, *params.Available)
    }
    if params.Blocked != nil {
        where = append(where, "blocked = ?")
        args = append(args, *params.Blocked)
    }
    if params.Source != "" {
        where = append(where, "source LIKE ?")
        args = append(args, "%"+params.Source+"%")
    }
    
    whereClause := strings.Join(where, " AND ")
    
    // 查询总数
    var total int
    err := s.db.QueryRow(
        "SELECT COUNT(*) FROM nodes WHERE "+whereClause,
        args...,
    ).Scan(&total)
    if err != nil {
        return nil, err
    }
    
    // 构建ORDER BY子句
    orderBy := "latency ASC"  // 默认排序
    if params.SortBy != "" {
        orderBy = params.SortBy + " " + strings.ToUpper(params.SortOrder)
    }
    
    // 分页查询
    offset := (params.Page - 1) * params.PageSize
    query := fmt.Sprintf(
        "SELECT * FROM nodes WHERE %s ORDER BY %s LIMIT ? OFFSET ?",
        whereClause, orderBy,
    )
    args = append(args, params.PageSize, offset)
    
    rows, err := s.db.Query(query, args...)
    // ... 扫描结果
    
    return &NodeQueryResult{
        Nodes:      nodes,
        Total:      total,
        Page:       params.Page,
        PageSize:   params.PageSize,
        TotalPages: (total + params.PageSize - 1) / params.PageSize,
    }, nil
}
```

### 前端Vue组件

```vue
<template>
  <el-card>
    <!-- 筛选区 -->
    <div class="filter-section">
      <el-select v-model="filters.region" placeholder="地区">
        <el-option label="全部" value="" />
        <el-option label="🇭🇰 香港" value="HK" />
        <!-- ... -->
      </el-select>
      <el-select v-model="filters.type" placeholder="类型">
        <el-option label="全部" value="" />
        <el-option label="VMess" value="vmess" />
        <!-- ... -->
      </el-select>
      <el-select v-model="filters.available" placeholder="状态">
        <el-option label="全部" :value="null" />
        <el-option label="可用" :value="true" />
        <el-option label="不可用" :value="false" />
      </el-select>
      <el-input
        v-model="filters.source"
        placeholder="搜索机场"
        clearable
      />
      <el-button @click="resetFilters">重置</el-button>
    </div>
    
    <!-- 批量操作区 -->
    <div class="batch-section">
      <span>已选 {{ selection.length }} 项</span>
      <el-button @click="blockSelected">屏蔽选中</el-button>
      <el-button @click="unblockSelected">取消屏蔽</el-button>
    </div>
    
    <!-- 表格 -->
    <el-table :data="nodes" @selection-change="handleSelectionChange">
      <el-table-column type="selection" />
      <el-table-column prop="name" label="原始名称" />
      <el-table-column prop="display_name" label="标准名称" />
      <el-table-column prop="type" label="类型" />
      <el-table-column prop="region" label="地区" />
      <el-table-column prop="latency" label="延迟" />
      <el-table-column label="状态" />
      <el-table-column prop="source" label="来源" />
      <el-table-column label="操作" />
    </el-table>
    
    <!-- 分页 -->
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.page_size"
      :total="pagination.total"
      :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next, jumper"
      @current-change="loadNodes"
      @size-change="loadNodes"
    />
  </el-card>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import client from '@/api/client'

const filters = reactive({
  region: '',
  type: '',
  available: null,
  blocked: null,
  source: '',
})

const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0,
})

const nodes = ref([])
const selection = ref([])

// 监听筛选条件变化,重置到第1页
watch(filters, () => {
  pagination.page = 1
  loadNodes()
})

const loadNodes = async () => {
  const params = {
    page: pagination.page,
    page_size: pagination.page_size,
    ...filters,
  }
  const res = await client.get('/api/nodes', { params })
  nodes.value = res.nodes
  pagination.total = res.total
}

// ...其他方法
</script>
```

## 权衡

### 为什么用后端分页而非前端分页?

**后端分页的优势**:
- 减少单次请求数据量(1000个节点 vs 20个节点)
- 数据库层面筛选和排序,性能更好
- 支持模糊搜索(LIKE查询)

**前端分页的劣势**:
- 一次性加载全部节点,首次加载慢
- 前端内存占用高
- 筛选/排序在JavaScript中执行,性能差

### 为什么补全地区映射表?

**当前问题**: 节点被识别为"Unknown",无法按地区筛选

**解决方案**: 补全常见地区的映射规则
- 印度/俄罗斯/荷兰等地区的机场逐渐增多
- 补全后覆盖率从85%提升到95%+
- 减少"Unknown"节点的比例

**维护成本**: 低(每个地区只需4-5条规则)

## 后果

### 正面

- **性能大幅提升**: 首次加载时间从5s降到<500ms
- **筛选更精准**: 多维度组合筛选,快速定位目标节点
- **UI更清晰**: 三段式布局,操作逻辑一目了然
- **地区识别率提升**: Unknown节点减少50%+

### 负面

- **后端改造成本**: 需要修改API,增加分页逻辑
- **前端改造成本**: 需要重写Nodes.vue组件
- **数据库迁移**: 需要补全region_rules表
- **测试工作量**: 需要测试各种筛选/排序/分页组合

## 测试场景

### 分页查询

```
场景: 1234个节点,每页20条
请求: GET /api/nodes?page=1&page_size=20
预期: 返回前20个节点,total=1234,total_pages=62

场景: 跳转到最后一页
请求: GET /api/nodes?page=62&page_size=20
预期: 返回最后14个节点(1234 % 20 = 14)
```

### 筛选组合

```
场景: 筛选香港的可用vmess节点
请求: GET /api/nodes?region=HK&type=vmess&available=true
预期: 只返回满足3个条件的节点

场景: 搜索机场名包含"极速"
请求: GET /api/nodes?source=极速
预期: 返回source字段包含"极速"的节点
```

### 排序

```
场景: 按延迟升序
请求: GET /api/nodes?sort_by=latency&sort_order=asc
预期: 节点按延迟从低到高排序

场景: 按地区降序
请求: GET /api/nodes?sort_by=region&sort_order=desc
预期: 节点按地区字母倒序(US, SG, HK...)
```

## 参考

- Element Plus Table 组件文档
- Element Plus Pagination 组件文档
- SQL LIMIT/OFFSET 分页最佳实践
