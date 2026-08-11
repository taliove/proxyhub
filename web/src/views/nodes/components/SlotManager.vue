<template>
  <div class="slot-manager">
    <div class="slot-header">
      <span class="slot-title">
        名称槽位
        <el-tooltip
          content="名称是你的资产：一次起名，所有订阅地址统一生效；节点不可用时把名称转移给别的节点。"
          placement="top"
        >
          <el-icon class="muted"><QuestionFilled /></el-icon>
        </el-tooltip>
      </span>
      <el-button size="small" type="primary" plain @click="createNew">新建名称</el-button>
    </div>

    <el-table v-loading="loading" :data="slots" size="small" row-key="name">
      <el-table-column label="名称" min-width="160">
        <template #default="{ row }">
          <span class="slot-name">{{ row.name }}</span>
        </template>
      </el-table-column>
      <el-table-column label="挂载节点" min-width="200">
        <template #default="{ row }">
          <el-tag v-if="row.empty" type="danger" size="small">空槽 · 待指派</el-tag>
          <template v-else-if="row.node">
            <span v-if="row.node.missing" class="muted">节点已消失（{{ row.node_key }}）</span>
            <span v-else>{{ row.node.name }}</span>
          </template>
        </template>
      </el-table-column>
      <el-table-column label="来源" min-width="110">
        <template #default="{ row }">
          <span v-if="row.node && !row.node.missing">{{ row.node.source }}</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.empty" type="danger" size="small" effect="plain">空槽</el-tag>
          <el-tag v-else-if="row.node?.missing || row.node?.stale" type="danger" size="small">
            已消失
          </el-tag>
          <el-tag v-else-if="row.node && !row.node.available" type="warning" size="small">
            不可用
          </el-tag>
          <el-tag v-else type="success" size="small" effect="plain">在线</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="最近探测" width="120">
        <template #default="{ row }">
          <span v-if="row.node?.last_probe_at" class="probe-cell">
            <span :class="row.node.last_probe_ok ? 'probe-ok' : 'probe-fail'">
              {{ row.node.last_probe_ok ? '✓' : '✗' }}
            </span>
            <span class="muted num probe-time">{{ shortProbeTime(row.node.last_probe_at) }}</span>
          </span>
          <span v-else-if="!monitorEnabled" class="muted">监控未开启</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="24 小时" width="230">
        <template #default="{ row }">
          <ProbeGrid v-if="row.probe_grid" :grid="row.probe_grid" />
          <span v-else-if="!monitorEnabled" class="muted">设置 → 告警设置里开启</span>
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="200">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="openAssign(row)">
            {{ row.empty || row.node?.missing || row.node?.stale ? '指派节点' : '换人' }}
          </el-button>
          <el-button link size="small" @click="rename(row)">改名</el-button>
          <el-button link type="danger" size="small" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 迁移待处理冲突:同名竞争落选行,认领(改成槽位)或放弃(清除覆盖) -->
    <div v-if="conflicts.length" class="conflict-zone">
      <div class="conflict-title">待处理冲突（旧名称覆盖同名竞争落选）</div>
      <div v-for="c in conflicts" :key="c.node_key" class="conflict-row">
        <span class="conflict-name">{{ c.display_name }}</span>
        <span class="muted">{{ c.node_key }}</span>
        <span class="conflict-ops">
          <el-button link type="primary" size="small" @click="claim(c)">认领为槽位</el-button>
          <el-button link type="danger" size="small" @click="drop(c)">放弃</el-button>
        </span>
      </div>
    </div>

    <SlotAssignNodeDialog ref="assignDialog" :nodes="nodes" @saved="emit('changed')" />

    <!-- 新建/改名:带变量插入与实时预览的编辑器(改名按当前挂载节点渲染) -->
    <el-dialog v-model="editVisible" :title="editTitle" width="480px">
      <SlotNameEditor v-model="editName" :node-key="editNodeKey" placeholder="输入名称" />
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!editName" @click="saveEdit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
import client from '@/api/client'
import ProbeGrid from './ProbeGrid.vue'
import SlotNameEditor from './SlotNameEditor.vue'
import SlotAssignNodeDialog from './SlotAssignNodeDialog.vue'
import {
  createSlot,
  deleteSlot,
  readSlotConflict,
  updateSlot,
  type NameSlot,
  type SlotConflictRow
} from '@/api/slots'
import { apiErrorMessage } from '../utils'
import type { UnifiedNode } from '../selfmerge'

const props = defineProps<{
  slots: NameSlot[]
  conflicts: SlotConflictRow[]
  loading: boolean
  // 监控总开关(订阅节点监控):关时探测列显示指引而非横杠
  monitorEnabled: boolean
  // 指派候选:统一行集(机场+自建)
  nodes: UnifiedNode[]
}>()

const emit = defineEmits<{
  (e: 'changed'): void
}>()

// 探测时间紧凑格式:今天只显示时分秒,跨天带月-日(列宽窄,防换行)
const shortProbeTime = (s: string): string => {
  const today = new Date().toISOString().slice(0, 10)
  const day = s.slice(0, 10)
  const time = s.slice(11, 19)
  return day === today ? time : `${s.slice(5, 10)} ${s.slice(11, 16)}`
}

// 新建/改名编辑器对话框
const editVisible = ref(false)
const editMode = ref<'create' | 'rename' | 'claim'>('create')
const editTarget = ref<NameSlot | null>(null)
const editName = ref('')
const editTitle = computed(() => {
  if (editMode.value === 'create') return '新建名称'
  if (editMode.value === 'claim') return `认领为槽位 - ${claimRow.value?.display_name || ''}`
  return `改名 - ${editTarget.value?.name || ''}`
})
// 预览上下文:改名=当前挂载节点;认领=冲突行节点;新建空槽=无(指派后可预览)
const editNodeKey = computed(() => editTarget.value?.node_key ?? claimRow.value?.node_key ?? '')

const createNew = () => {
  editMode.value = 'create'
  editTarget.value = null
  editName.value = ''
  editVisible.value = true
}

const saveEdit = async () => {
  const name = editName.value
  if (editMode.value === 'claim' && claimRow.value) {
    await doClaim(claimRow.value, name)
    return
  }
  try {
    if (editMode.value === 'create') {
      await createSlot(name)
      ElMessage.success('已创建空槽，可指派节点')
    } else if (editTarget.value) {
      await updateSlot(editTarget.value.name, { newName: name })
      ElMessage.success('已改名，立即生效')
    }
    editVisible.value = false
    emit('changed')
  } catch (e) {
    if (readSlotConflict(e)?.kind === 'name_taken') {
      ElMessage.error(`名称「${name}」已存在`)
    } else {
      ElMessage.error(apiErrorMessage(e, '保存失败'))
    }
  }
}

const assignDialog = ref<InstanceType<typeof SlotAssignNodeDialog> | null>(null)
const openAssign = (row: NameSlot) => assignDialog.value?.open(row)

const rename = (row: NameSlot) => {
  editMode.value = 'rename'
  editTarget.value = row
  editName.value = row.name
  editVisible.value = true
}

const remove = async (row: NameSlot) => {
  try {
    await ElMessageBox.confirm(`删除名称「${row.name}」？挂载节点回退模板/原始名称。`, '删除名称', {
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await deleteSlot(row.name)
    ElMessage.success('已删除')
    emit('changed')
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '删除失败'))
  }
}

// 冲突认领:复用编辑器对话框(带变量插入/预览),落库逻辑保留原 force 确认链
const claimRow = ref<SlotConflictRow | null>(null)
const claim = (c: SlotConflictRow) => {
  claimRow.value = c
  editMode.value = 'claim'
  editTarget.value = null
  editName.value = c.display_name
  editVisible.value = true
}

const doClaim = async (c: SlotConflictRow, name: string) => {
  try {
    await createSlot(name, c.node_key)
  } catch (e) {
    const conflict = readSlotConflict(e)
    if (conflict?.kind === 'node_occupied') {
      try {
        await ElMessageBox.confirm(
          `该节点当前挂在名称「${conflict.holder_name}」上，改挂后旧名称变空槽。确认？`,
          '转移确认',
          { type: 'warning' }
        )
        await createSlot(name, c.node_key, true)
      } catch {
        return
      }
    } else if (conflict?.kind === 'name_taken') {
      ElMessage.error(`名称「${name}」已存在`)
      return
    } else {
      ElMessage.error(apiErrorMessage(e, '认领失败'))
      return
    }
  }
  await clearOverrideRow(c.node_key)
  ElMessage.success('已认领')
  editVisible.value = false
  emit('changed')
}

// 放弃:清掉覆盖行(display_name 残留随行的去留规则清除,收藏保留)
const drop = async (c: SlotConflictRow) => {
  try {
    await ElMessageBox.confirm(
      `放弃节点 ${c.node_key} 的旧名称「${c.display_name}」？该名称仍被别的节点占用。`,
      '放弃覆盖',
      { type: 'warning' }
    )
  } catch {
    return
  }
  await clearOverrideRow(c.node_key)
  ElMessage.success('已放弃')
  emit('changed')
}

const clearOverrideRow = async (nodeKey: string) => {
  try {
    await client.delete('/nodes/override', { data: { node_key: nodeKey } })
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '清理覆盖失败'))
  }
}
</script>

<style scoped>
.slot-manager {
  margin-bottom: var(--ph-space-4);
}
.slot-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: var(--ph-space-3);
}
.slot-title {
  font-weight: 500;
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.slot-name {
  font-weight: 500;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.option-meta {
  float: right;
}
.ctl-full {
  width: 100%;
}
.conflict-zone {
  margin-top: var(--ph-space-3);
  border: 1px dashed var(--ph-warning);
  border-radius: var(--ph-radius-sm);
  padding: var(--ph-space-2) var(--ph-space-3);
}
.conflict-title {
  font-size: var(--ph-text-sm);
  color: var(--ph-warning);
  margin-bottom: var(--ph-space-2);
}
.conflict-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-1) 0;
}
.conflict-name {
  font-weight: 500;
}
.conflict-ops {
  margin-left: auto;
}
.probe-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.probe-ok {
  color: var(--ph-success);
}
.probe-fail {
  color: var(--ph-danger);
}
.num {
  font-variant-numeric: tabular-nums;
}
</style>
