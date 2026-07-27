<template>
  <div>
    <div class="target-toolbar">
      <el-button type="primary" @click="addTarget">添加目标</el-button>
      <el-button @click="loadTargets">刷新</el-button>
    </div>
    <el-table :data="detectionTargets" border>
      <el-table-column prop="name" label="名称" width="120">
        <template #default="{ row }">
          <el-input v-model="row.name" size="small" />
        </template>
      </el-table-column>
      <el-table-column prop="kind" label="类型" width="130">
        <template #default="{ row }">
          <el-tag size="small" :type="row.kind && row.kind !== 'generic' ? 'warning' : 'info'">
            {{ row.kind && row.kind !== 'generic' ? row.kind : 'generic' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="url" label="URL" min-width="200">
        <template #default="{ row }">
          <el-input v-model="row.url" size="small" />
        </template>
      </el-table-column>
      <el-table-column prop="method" label="方法" width="80">
        <template #default="{ row }">
          <el-select v-model="row.method" size="small">
            <el-option label="GET" value="GET" />
            <el-option label="POST" value="POST" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column prop="expect_status" label="期望状态码" width="150">
        <template #default="{ row }">
          <el-input v-model="row.expect_status_str" size="small" placeholder="200,204" />
        </template>
      </el-table-column>
      <el-table-column prop="response_excludes" label="排除关键字" width="180">
        <template #default="{ row }">
          <el-input v-model="row.response_excludes_str" size="small" placeholder="逗号分隔" />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ $index }">
          <el-button type="danger" size="small" link @click="removeTarget($index)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <div class="target-actions">
      <el-button type="primary" @click="saveTargets">保存配置</el-button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'

// 检测目标配置(租户级设置,见 CONTEXT.md「租户级设置」):
// 后端按视角分流——超管未 impersonate 编辑全局默认,普通用户编辑本人覆盖。
interface DetectionTarget {
  name: string
  // 检测类型:空/generic=通用判定,其余(netflix 等)=专用解锁判定。
  // UI 只读展示并原样回传,避免保存时丢失播种目标的 kind。
  kind?: string
  url: string
  method: string
  headers: Record<string, string>
  expect_status: number[]
  response_contains: string[]
  response_excludes: string[]
  // UI 辅助字段(数组转逗号字符串)
  expect_status_str?: string
  response_excludes_str?: string
}

const detectionTargets = ref<DetectionTarget[]>([])

onMounted(async () => {
  await loadTargets()
})

const loadTargets = async () => {
  const data = await client.get<unknown, DetectionTarget[]>('/settings/detection-targets')
  // 转换数组为字符串(便于编辑)
  detectionTargets.value = data.map((t: DetectionTarget) => ({
    ...t,
    headers: t.headers || {},
    expect_status_str: (t.expect_status || []).join(','),
    response_excludes_str: (t.response_excludes || []).join(',')
  }))
}

const saveTargets = async () => {
  // 转换字符串回数组
  const payload = detectionTargets.value.map((t) => ({
    name: t.name,
    // 原样回传 kind(专用解锁目标),缺省交由后端按 generic 处理
    ...(t.kind ? { kind: t.kind } : {}),
    url: t.url,
    method: t.method,
    headers: t.headers || {},
    expect_status: (t.expect_status_str || '')
      .split(',')
      .map((s) => parseInt(s.trim()))
      .filter((n) => !isNaN(n)),
    response_contains: t.response_contains || [],
    response_excludes: (t.response_excludes_str || '')
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s)
  }))
  await client.put('/settings/detection-targets', payload)
  ElMessage.success('保存成功')
  await loadTargets()
}

const addTarget = () => {
  detectionTargets.value.push({
    name: '',
    url: '',
    method: 'GET',
    headers: {},
    expect_status: [200],
    response_contains: [],
    response_excludes: [],
    expect_status_str: '200',
    response_excludes_str: ''
  })
}

const removeTarget = (index: number) => {
  detectionTargets.value.splice(index, 1)
}
</script>

<style scoped>
.target-toolbar {
  display: flex;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-4);
}
.target-actions {
  margin-top: var(--ph-space-4);
}
</style>
