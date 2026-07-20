# 后台功能模块重组规范

## Problem Statement

当前后台管理界面的功能模块划分和菜单顺序存在以下问题：

1. **逻辑顺序混乱**：菜单顺序不符合用户的操作流程。用户应该先配置机场和节点（数据源），然后生成订阅地址（输出），但当前"订阅地址"排在第2位，机场管理排在第3位。

2. **节点功能分散**：节点相关的两个页面（"节点状态"和"自建节点"）被分离在不同位置，实际上都属于节点管理范畴，应该整合。

3. **监控分析功能分散**：刷新记录、访问统计、安全审计三个监控分析类功能分散在不同位置，不便于排查问题。

4. **命名不够清晰**：
   - "节点状态" 暗示只能查看，但实际上有管理功能
   - "刷新记录" 不够规范，应该称为"同步日志"
   - "配置模板" 与 "系统设置" 都有"配置"含义，容易混淆

5. **功能分组不明确**：缺乏视觉分隔，核心功能、监控功能、系统设置混在一起。

## Solution

重新组织后台菜单结构，使其符合用户的操作流程和认知习惯：

1. **调整菜单顺序**：按照"数据源 → 资源管理 → 输出配置 → 监控分析 → 系统设置"的流程排列
2. **合并节点管理**：将"节点状态"和"自建节点"合并为统一的"节点管理"页面，用 Tab 切换
3. **优化命名**：使用更清晰、更符合行业规范的名称
4. **视觉分组**：在监控功能和系统设置前添加视觉分隔

## User Stories

1. 作为管理员，我希望菜单顺序符合我的操作流程，这样我能更快找到需要的功能
2. 作为新用户，我希望从菜单结构就能理解系统的使用流程，这样我能快速上手
3. 作为管理员，我希望节点相关功能集中在一起，这样我不用在多个页面跳转
4. 作为管理员，我希望在节点管理页面能方便地切换查看机场节点和自建节点，这样我能统一管理所有节点
5. 作为管理员，我希望监控类功能（日志、统计、审计）集中展示，这样排查问题时能快速找到相关信息
6. 作为管理员，我希望菜单名称清晰明确，这样我不会混淆"配置模板"和"系统设置"的用途
7. 作为管理员，我希望能通过菜单分组区分核心功能和辅助功能，这样我能聚焦主要任务
8. 作为管理员，我希望在节点管理页面的 Tab 切换是即时的，这样我能快速对比机场节点和自建节点
9. 作为管理员，我希望合并后的页面保留原有的所有功能，这样我的工作流程不会被打断
10. 作为开发者，我希望菜单配置集中在一处，这样后续维护更容易
11. 作为管理员，我希望重组后的菜单在移动端也能正常使用，这样我能在手机上管理系统
12. 作为管理员，我希望菜单重组后不影响书签和收藏的路由，这样我保存的链接仍然有效

## Implementation Decisions

### 1. 新菜单结构

**重组后的菜单顺序**：

```
1. 仪表盘 (/)
2. 机场管理 (/airports)
3. 节点管理 (/nodes) - 合并页面，包含两个 Tab
   - Tab: 机场节点（原"节点状态"）
   - Tab: 自建节点（原"自建节点"）
4. 我的订阅 (/endpoints) - 重命名自"订阅地址"
5. 订阅模板 (/template) - 重命名自"配置模板"
6. ─────────── (视觉分隔)
7. 流量统计 (/stats) - 重命名自"访问统计"
8. 同步日志 (/refresh-log) - 重命名自"刷新记录"
9. 安全审计 (/audit)
10. ───────────
11. 系统设置 (/settings)
```

**命名变更映射**：
- 订阅地址 → **我的订阅**（更突出管理功能）
- 节点状态 → **节点管理**（避免"只读"误解）
- 配置模板 → **订阅模板**（明确是生成订阅用的模板）
- 访问统计 → **流量统计**（更明确统计内容）
- 刷新记录 → **同步日志**（符合日志命名规范）

**路由保持不变**：URL 路径不变，只修改显示名称和顺序，确保已有书签和外部链接不受影响。

### 2. 节点管理页面合并

**技术方案**：

- **保留路由**：`/nodes` 作为主路由，移除 `/self-nodes` 独立路由
- **Tab 实现**：使用 Element Plus 的 `<el-tabs>` 组件
- **状态管理**：当前 Tab 状态存储在 URL query 参数（`?tab=self`），支持直接链接和浏览器前进/后退
- **组件复用**：将当前 `Nodes.vue` 和 `SelfNodes.vue` 的核心逻辑提取为可复用的子组件

**新页面结构**：

```vue
<template>
  <el-card>
    <template #header>
      <span>节点管理</span>
    </template>
    
    <el-tabs v-model="activeTab" @tab-change="handleTabChange">
      <el-tab-pane label="机场节点" name="airport">
        <AirportNodesTable />
      </el-tab-pane>
      <el-tab-pane label="自建节点" name="self">
        <SelfNodesTable />
      </el-tab-pane>
    </el-tabs>
  </el-card>
</template>
```

**组件拆分**：

- `web/src/views/Nodes.vue` → 合并后的主页面（包含 Tab）
- `web/src/components/AirportNodesTable.vue` → 机场节点表格（原 Nodes.vue 的核心部分）
- `web/src/components/SelfNodesTable.vue` → 自建节点表格（原 SelfNodes.vue 的核心部分）
- `web/src/views/SelfNodes.vue` → 删除（功能已迁移）

### 3. 路由配置更新

修改 `web/src/router/index.ts`：

**移除的路由**：
```typescript
{ path: 'self-nodes', name: 'SelfNodes', ... }  // 删除独立路由
```

**调整的路由顺序**：
```typescript
children: [
  { path: '', name: 'Dashboard', meta: { title: '仪表盘', icon: 'Monitor' } },
  { path: 'airports', name: 'Airports', meta: { title: '机场管理', icon: 'Connection' } },
  { path: 'nodes', name: 'Nodes', meta: { title: '节点管理', icon: 'Cpu' } },  // 合并后的页面
  { path: 'endpoints', name: 'Endpoints', meta: { title: '我的订阅', icon: 'Link' } },
  { path: 'template', name: 'Template', meta: { title: '订阅模板', icon: 'Document' } },
  { path: 'stats', name: 'Stats', meta: { title: '流量统计', icon: 'TrendCharts' } },
  { path: 'refresh-log', name: 'RefreshLog', meta: { title: '同步日志', icon: 'Refresh' } },
  { path: 'audit', name: 'Audit', meta: { title: '安全审计', icon: 'Warning' } },
  { path: 'settings', name: 'Settings', meta: { title: '系统设置', icon: 'Setting' } }
]
```

### 4. 视觉分隔实现

**方案**：在导航组件中根据特定路径添加分隔线

修改 `web/src/layout/nav.ts` 的 `NavItem` 接口：

```typescript
export interface NavItem {
  path: string
  title: string
  icon: string
  divider?: boolean  // 新增：在此项前显示分隔线
}
```

在 `getMenuItems` 函数中标记分隔位置：

```typescript
export function getMenuItems(router: Router): NavItem[] {
  const root = router.options.routes.find((r) => r.path === HOME_PATH)
  const children = root?.children ?? []
  return children
    .filter((c) => c.meta && c.meta.title)
    .map((c) => ({
      path: toAbsolutePath(c.path),
      title: c.meta!.title as string,
      icon: (c.meta!.icon as string) || 'Menu',
      divider: c.path === 'stats' || c.path === 'settings'  // 在监控和设置前加分隔线
    }))
}
```

### 5. 后端 API 保持不变

此次重组**纯前端改动**，后端 API 不需要修改：

- `/api/nodes` - 机场节点列表（已实现分页和筛选，见 ADR 0013）
- `/api/self-nodes` - 自建节点列表
- 其他 API 端点保持不变

### 6. 向后兼容处理

为了兼容可能存在的外部链接和书签：

**添加路由重定向**：

```typescript
{
  path: '/self-nodes',
  redirect: '/nodes?tab=self'  // 自动跳转到节点管理的自建节点 Tab
}
```

## Testing Decisions

### 测试原则

- **测试外部行为，不测试实现细节**：测试用户可见的菜单顺序、名称、路由跳转，不测试内部状态变量
- **优先 E2E 测试**：菜单重组是 UI 改动，使用 E2E 测试验证用户流程
- **单元测试覆盖核心逻辑**：导航生成函数、路由配置的单元测试

### 测试模块

#### 1. 路由配置测试

**文件**：`web/src/router/index.test.ts`（新建）

**测试内容**：
- 路由顺序正确（airports 在 nodes 之前，nodes 在 endpoints 之前）
- 路由 meta 信息正确（title、icon）
- `/self-nodes` 重定向到 `/nodes?tab=self`

**参考**：项目中暂无前端测试，需要建立 Vitest 测试框架

#### 2. 导航生成测试

**文件**：`web/src/layout/nav.test.ts`（新建）

**测试内容**：
- `getMenuItems()` 返回的菜单项顺序正确
- 菜单项数量正确（合并后应为 9 项）
- 分隔线标记正确（`divider` 字段）

#### 3. 节点管理页面测试

**文件**：`web/src/views/Nodes.test.ts`（新建）

**测试内容**：
- Tab 切换功能正常
- URL query 参数同步（切换 Tab 时 URL 更新为 `?tab=self`）
- 浏览器前进/后退按钮正常工作
- 默认显示"机场节点" Tab

#### 4. E2E 测试

**文件**：`tests/e2e/menu-navigation.spec.ts`（新建）

**测试场景**：
- 用户登录后，侧边栏菜单按新顺序显示
- 点击"节点管理"，进入机场节点页面
- 点击"自建节点" Tab，切换到自建节点视图
- 直接访问 `/self-nodes`，自动重定向到 `/nodes?tab=self`
- 刷新页面，Tab 状态保持
- 点击侧边栏其他菜单项，导航到正确页面

**工具**：Playwright（如果项目已有）或 Cypress

**参考案例**：项目中已有后端测试（`*_test.go`），但前端测试需要新建

### 测试覆盖率目标

- 路由配置：100%（所有路由都有测试）
- 导航生成：100%（核心逻辑简单，易于覆盖）
- 节点管理页面：80%+（覆盖主要用户流程）
- E2E 测试：覆盖所有关键用户场景

## Out of Scope

以下内容不在本次重组范围内：

1. **二级菜单实现**：不实现嵌套菜单结构，保持扁平化一级菜单（未来可优化）
2. **仪表盘快捷操作**：不在仪表盘添加快捷操作按钮（未来可优化）
3. **统计功能整合**：不合并 Endpoints 页面内的 IP 统计和独立的流量统计页面（功能重复，未来可整合）
4. **图标更新**：不修改菜单图标，除非现有图标明显不匹配（保持一致性）
5. **移动端适配**：不专门为移动端优化菜单布局（Element Plus 默认响应式已够用）
6. **深色模式**：不调整深色模式下的分隔线样式（使用组件库默认）
7. **国际化**：不添加多语言支持（当前系统为中文）
8. **后端改动**：不修改任何后端 API、数据库结构、业务逻辑
9. **权限控制**：不添加基于角色的菜单显示控制（当前系统只有管理员角色）
10. **菜单搜索**：不添加菜单项搜索功能（项目规模小，无需此功能）

## Further Notes

### 实施优先级

**第一阶段（核心改动）**：
1. 修改路由配置（`router/index.ts`）- 调整顺序、重命名、添加重定向
2. 拆分节点管理组件（`components/AirportNodesTable.vue`、`SelfNodesTable.vue`）
3. 合并节点页面（`views/Nodes.vue` 添加 Tab）
4. 更新导航生成逻辑（`layout/nav.ts` 添加分隔线支持）

**第二阶段（视觉优化）**：
5. 实现分隔线样式
6. 验证所有页面功能正常

**第三阶段（测试和文档）**：
7. 编写单元测试和 E2E 测试
8. 更新用户文档（如果有）

### 潜在风险和缓解措施

**风险 1**：用户已经习惯旧的菜单顺序，重组后可能暂时找不到功能

**缓解**：
- 在系统更新日志中明确说明菜单调整
- 保留路由重定向，旧链接仍然有效
- 新菜单顺序更符合逻辑，用户适应期短

**风险 2**：合并节点页面可能导致页面加载变慢（两个 Tab 的数据都需要加载）

**缓解**：
- 使用懒加载：只加载当前激活 Tab 的数据
- Tab 切换时再加载另一个 Tab 的数据
- 利用 ADR 0013 的分页优化，节点数据已经很快

**风险 3**：组件拆分可能引入新的 bug

**缓解**：
- 严格遵循原有逻辑，只做结构重组，不改变功能
- 充分测试筛选、批量操作、解锁检测等核心功能
- 保留原 `Nodes.vue` 的 git 历史，便于回溯

### 参考 ADR

- **ADR 0013**：节点管理分页与筛选系统 - 本次合并的机场节点页面已实现分页和高级筛选
- **ADR 0012**：节点名称标准化 - 节点命名逻辑不受此次重组影响

### 术语表

- **机场**：订阅源，提供节点的服务商
- **机场节点**：从机场订阅获取的节点
- **自建节点**：用户手动添加的节点，作为 Failback 常驻注入订阅
- **订阅地址**：生成的聚合订阅链接，供客户端使用
- **订阅模板**：Clash 配置骨架（hosts/dns/proxy-groups/rules），用 `{{nodes}}` 占位符
- **同步日志**：机场刷新和节点聚合的历史记录
