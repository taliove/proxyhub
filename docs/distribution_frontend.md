# 流量分发功能 - 前端设计

## 菜单位置

在路由中新增独立一级菜单：

```typescript
// router/index.ts
{
  path: '/',
  component: () => import('@/layout/index.vue'),
  children: [
    // ... 现有路由
    { 
      path: 'distribution', 
      name: 'Distribution', 
      component: () => import('@/views/Distribution.vue'), 
      meta: { 
        title: '流量分发', 
        icon: 'Share' 
      } 
    },
    // ... 其他路由
  ]
}
```

菜单顺序建议：
```
仪表盘
机场管理
节点管理
我的订阅
订阅模板
流量分发          ← 新增，放在"订阅模板"后
─────────
流量统计
同步日志
安全审计
─────────
系统设置
```

## 页面结构

### Distribution.vue (主页面)

使用 Tab 组件分为三个子页面：
1. **全局配置** - 配置代理协议、端口、域名、TLS 等
2. **分发路径** - 管理各个 Path 及其对应的节点池
3. **流量统计** - 查看各个 Path 的流量使用情况

```vue
<template>
  <div class="distribution-container">
    <el-card>
      <template #header>
        <div class="header-wrapper">
          <span>流量分发</span>
          <el-switch
            v-model="globalEnabled"
            @change="handleGlobalToggle"
            active-text="已启用"
            inactive-text="已禁用"
          />
        </div>
      </template>

      <el-tabs v-model="activeTab" type="border-card">
        <el-tab-pane label="全局配置" name="config">
          <GlobalConfig :config="config" @update="handleConfigUpdate" />
        </el-tab-pane>
        
        <el-tab-pane label="分发路径" name="paths">
          <DistributionPaths :paths="paths" @refresh="loadPaths" />
        </el-tab-pane>
        
        <el-tab-pane label="流量统计" name="stats">
          <DistributionStats :paths="paths" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getDistributionConfig, updateDistributionConfig } from '@/api/distribution'
import GlobalConfig from './components/GlobalConfig.vue'
import DistributionPaths from './components/DistributionPaths.vue'
import DistributionStats from './components/DistributionStats.vue'

const activeTab = ref('config')
const globalEnabled = ref(false)
const config = ref(null)
const paths = ref([])

// ... 逻辑实现
</script>
```

## 子组件设计

### 1. GlobalConfig.vue (全局配置)

**功能**：
- 配置监听端口、域名
- 选择协议（VLESS/VMess）
- 选择传输方式（gRPC/WebSocket）
- 配置 UUID（带生成按钮）
- 配置 TLS 证书路径

**布局**：
```
┌─────────────────────────────────────────────────┐
│ 全局配置                                         │
├─────────────────────────────────────────────────┤
│                                                  │
│ 基础配置                                         │
│ ┌─────────────────────────────────────────────┐ │
│ │ 监听端口    [8443              ]            │ │
│ │ 域名        [proxy.example.com ]            │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
│ 协议配置                                         │
│ ┌─────────────────────────────────────────────┐ │
│ │ 协议        [VLESS ▼] [VMess ▼]            │ │
│ │ 传输方式    [gRPC ▼] [WebSocket ▼]         │ │
│ │ UUID        [uuid-here    ] [🔄 生成]      │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
│ TLS 配置                                        │
│ ┌─────────────────────────────────────────────┐ │
│ │ ☑ 启用 TLS                                  │ │
│ │ 证书路径    [/path/to/cert.pem]            │ │
│ │ 密钥路径    [/path/to/key.pem ]            │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
│ 提示信息                                         │
│ ┌─────────────────────────────────────────────┐ │
│ │ ℹ️ 所有分发路径将共享以上协议配置            │ │
│ │ ℹ️ 仅支持 gRPC 和 WebSocket 传输方式        │ │
│ │ ℹ️ 修改配置后需要重启 Xray 服务才能生效     │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
│ [保存配置]  [重启 Xray]                         │
│                                                  │
└─────────────────────────────────────────────────┘
```

**TypeScript 接口**：
```typescript
interface DistributionConfig {
  enabled: boolean
  listen_port: number
  domain: string
  protocol: 'vless' | 'vmess'
  network: 'grpc' | 'ws'
  uuid: string
  tls: boolean
  cert_path: string
  key_path: string
}
```

### 2. DistributionPaths.vue (分发路径管理)

**功能**：
- 列表展示所有分发路径
- 创建/编辑/删除路径
- 启用/禁用路径
- 选择上游节点（多选，支持从机场节点和自建节点中选择）
- 配置负载均衡策略

**布局**：
```
┌─────────────────────────────────────────────────┐
│ 分发路径管理                [+ 新建路径]         │
├─────────────────────────────────────────────────┤
│                                                  │
│ ┌─────────────────────────────────────────────┐ │
│ │ 📍 香港高速节点池                            │ │
│ │ Path: /hk                                   │ │
│ │ 上游节点: 3 个节点                          │ │
│ │   • 香港-01 (HK-Node1.example.com)         │ │
│ │   • 香港-02 (HK-Node2.example.com)         │ │
│ │   • 香港-03 (HK-Node3.example.com)         │ │
│ │ 负载策略: 轮询 (Round Robin)                │ │
│ │ 流量统计: ↑ 1.2GB  ↓ 5.6GB  🔗 1,234 次   │ │
│ │ 状态: ☑ 已启用                              │ │
│ │                                             │ │
│ │ [编辑] [删除] [查看统计]                    │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
│ ┌─────────────────────────────────────────────┐ │
│ │ 📍 美国节点池                                │ │
│ │ Path: /us                                   │ │
│ │ 上游节点: 2 个节点                          │ │
│ │   • 美国-洛杉矶 (US-LA.example.com)        │ │
│ │   • 美国-纽约 (US-NY.example.com)          │ │
│ │ 负载策略: 随机 (Random)                     │ │
│ │ 流量统计: ↑ 800MB  ↓ 3.2GB  🔗 567 次     │ │
│ │ 状态: ☑ 已启用                              │ │
│ │                                             │ │
│ │ [编辑] [删除] [查看统计]                    │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
└─────────────────────────────────────────────────┘
```

**创建/编辑对话框**：
```
┌─────────────────────────────────────────────────┐
│ 新建分发路径                         [✕]        │
├─────────────────────────────────────────────────┤
│                                                  │
│ 路径名称 *  [________________]                  │
│             (用于订阅中显示的节点名称)           │
│                                                  │
│ Path *      [________________]                  │
│             (如: /hk, /us, /sg)                 │
│                                                  │
│ 上游节点 *  [选择节点...     ▼]                │
│             ┌───────────────────────────────┐   │
│             │ ☑ 香港-01 (机场A)             │   │
│             │ ☑ 香港-02 (机场A)             │   │
│             │ ☐ 香港-03 (机场B)             │   │
│             │ ☑ HK-Self (自建)              │   │
│             │ ─────────────────             │   │
│             │ 已选择: 3 个节点               │   │
│             └───────────────────────────────┘   │
│                                                  │
│ 负载均衡    [轮询 ▼]                            │
│             (轮询 / 随机 / 最少连接)             │
│                                                  │
│ ☑ 启用此路径                                    │
│                                                  │
│             [取消]  [确定]                      │
│                                                  │
└─────────────────────────────────────────────────┘
```

**TypeScript 接口**：
```typescript
interface DistributionPath {
  id: number
  name: string
  path: string
  upstream_node_keys: string[]
  lb_strategy: 'round_robin' | 'random' | 'least_conn'
  total_upload: number
  total_download: number
  total_connections: number
  last_access: string | null
  enabled: boolean
}

interface NodeOption {
  label: string      // 节点名称
  value: string      // NodeKey
  source: string     // 来源（机场名称或"自建"）
  region: string     // 地区
  available: boolean // 是否可用
}
```

### 3. DistributionStats.vue (流量统计)

**功能**：
- 展示所有路径的流量统计汇总
- 时间范围筛选（今天/最近7天/最近30天/自定义）
- ECharts 图表展示流量趋势
- 详细数据表格

**布局**：
```
┌─────────────────────────────────────────────────┐
│ 流量统计                                         │
│                                                  │
│ 时间范围: [最近7天 ▼]  [2024-01-01] ~ [今天]   │
├─────────────────────────────────────────────────┤
│                                                  │
│ 概览卡片                                         │
│ ┌──────────┐ ┌──────────┐ ┌──────────┐        │
│ │ 总上传    │ │ 总下载    │ │ 总连接数  │        │
│ │ 2.5 GB   │ │ 12.3 GB  │ │ 3,456   │        │
│ └──────────┘ └──────────┘ └──────────┘        │
│                                                  │
│ 流量趋势图 (ECharts)                            │
│ ┌─────────────────────────────────────────────┐ │
│ │              📊 上传/下载流量趋势            │ │
│ │ GB                                          │ │
│ │  5 ┤        ╭─╮                            │ │
│ │  4 ┤      ╭─╯ ╰╮    ╭╮                     │ │
│ │  3 ┤    ╭─╯    ╰╮ ╭─╯╰╮                   │ │
│ │  2 ┤  ╭─╯       ╰─╯   ╰╮                  │ │
│ │  1 ┤╭─╯                ╰─╮                 │ │
│ │  0 ┼┴──┴──┴──┴──┴──┴──┴──┴                │ │
│ │    1/1 1/2 1/3 1/4 1/5 1/6 1/7            │ │
│ │                                            │ │
│ │ ── 下载   ── 上传                          │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
│ 各路径详细统计                                   │
│ ┌─────────────────────────────────────────────┐ │
│ │ Path     │ 上传    │ 下载    │ 连接数  │ 占比│ │
│ ├──────────┼─────────┼─────────┼────────┼────┤ │
│ │ /hk      │ 1.2 GB  │ 5.6 GB  │ 1,234  │45% │ │
│ │ /us      │ 800 MB  │ 3.2 GB  │ 567    │26% │ │
│ │ /sg      │ 500 MB  │ 3.5 GB  │ 1,655  │29% │ │
│ └─────────────────────────────────────────────┘ │
│                                                  │
└─────────────────────────────────────────────────┘
```

**TypeScript 接口**：
```typescript
interface StatsOverview {
  total_upload: number
  total_download: number
  total_connections: number
}

interface PathStatsDetail {
  path: string
  name: string
  upload: number
  download: number
  connections: number
  percentage: number
}

interface StatsTrend {
  timestamp: string
  upload: number
  download: number
  connections: number
}
```

## API 接口定义

```typescript
// api/distribution.ts

import request from './client'

// 全局配置
export function getDistributionConfig() {
  return request.get('/api/distribution/config')
}

export function updateDistributionConfig(data: DistributionConfig) {
  return request.put('/api/distribution/config', data)
}

// 分发路径
export function listDistributionPaths() {
  return request.get('/api/distribution/paths')
}

export function createDistributionPath(data: DistributionPath) {
  return request.post('/api/distribution/paths', data)
}

export function updateDistributionPath(id: number, data: DistributionPath) {
  return request.put(`/api/distribution/paths/${id}`, data)
}

export function deleteDistributionPath(id: number) {
  return request.delete(`/api/distribution/paths/${id}`)
}

export function toggleDistributionPath(id: number) {
  return request.post(`/api/distribution/paths/${id}/toggle`)
}

// 流量统计
export function getDistributionStats(params: { from?: string; to?: string }) {
  return request.get('/api/distribution/stats', { params })
}

export function getPathStats(id: number, params: { from?: string; to?: string }) {
  return request.get(`/api/distribution/paths/${id}/stats`, { params })
}

// Xray 管理
export function restartXray() {
  return request.post('/api/distribution/xray/restart')
}

export function getXrayStatus() {
  return request.get('/api/distribution/xray/status')
}
```

## 节点选择器组件

为了让用户方便地选择上游节点，需要一个节点选择器组件：

```vue
<!-- components/NodeSelector.vue -->
<template>
  <el-select
    v-model="selectedKeys"
    multiple
    filterable
    placeholder="选择上游节点"
    style="width: 100%"
  >
    <el-option-group
      v-for="group in groupedNodes"
      :key="group.label"
      :label="group.label"
    >
      <el-option
        v-for="node in group.options"
        :key="node.value"
        :label="node.label"
        :value="node.value"
        :disabled="!node.available"
      >
        <span>{{ node.label }}</span>
        <span style="float: right; color: #8492a6; font-size: 13px">
          {{ node.region }} | {{ node.available ? '可用' : '不可用' }}
        </span>
      </el-option>
    </el-option-group>
  </el-select>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { listNodes } from '@/api/nodes'

const selectedKeys = defineModel<string[]>({ required: true })
const allNodes = ref([])

// 按来源分组
const groupedNodes = computed(() => {
  const groups = new Map<string, NodeOption[]>()
  
  allNodes.value.forEach(node => {
    const source = node.source === 'self-hosted' ? '自建节点' : node.source
    if (!groups.has(source)) {
      groups.set(source, [])
    }
    groups.get(source)!.push({
      label: node.name,
      value: node.node_key,
      source: node.source,
      region: node.region,
      available: node.available
    })
  })
  
  return Array.from(groups.entries()).map(([label, options]) => ({
    label,
    options
  }))
})

onMounted(async () => {
  const res = await listNodes()
  allNodes.value = res.nodes
})
</script>
```

## 交互流程

### 1. 初次配置流程
```
1. 用户进入"流量分发"页面
   ↓
2. 填写全局配置（域名、端口、协议、UUID、证书）
   ↓
3. 保存配置
   ↓
4. 创建第一个分发路径（选择节点、设置 Path）
   ↓
5. 启用流量分发（开关）
   ↓
6. 系统启动 Xray 进程
   ↓
7. 提示用户：配置已生效，可在"我的订阅"中拉取包含分发节点的订阅
```

### 2. 修改配置流程
```
1. 用户修改全局配置或路径配置
   ↓
2. 保存配置
   ↓
3. 系统提示：需要重启 Xray 才能生效
   ↓
4. 用户点击"重启 Xray"
   ↓
5. 系统重新生成 Xray 配置并重启进程
   ↓
6. 提示重启成功
```

### 3. 查看统计流程
```
1. 用户进入"流量统计" Tab
   ↓
2. 选择时间范围
   ↓
3. 系统加载统计数据并渲染图表
   ↓
4. 用户可以点击具体路径查看详细统计
```

## 图标建议

- 全局配置: `Setting`
- 分发路径: `Share` 或 `Connection`
- 流量统计: `TrendCharts` 或 `DataLine`
- 上游节点: `Cpu` 或 `Monitor`
- 负载均衡: `Scale`
- 启用/禁用: `Switch`
