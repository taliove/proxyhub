<template>
  <div class="slot-manager">
    <div class="slot-header">
      <span class="slot-title">
        名称槽位
        <el-tooltip
          content="名称是你的资产:一次起名,所有订阅地址统一生效;节点不可用时把名称转移给别的节点。"
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
            <span v-if="row.node.missing" class="muted">节点已消失({{ row.node_key }})</span>
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
      <!-- 最近监控探测(issue #103):无监控数据走占位 -->
      <el-table-column label="最近探测" width="150">
        <template #default="{ row }">
          <span v-if="row.node?.last_probe_at" class="probe-cell">
            <span :class="row.node.last_probe_ok ? 'probe-ok' : 'probe-fail'">
              {{ row.node.last_probe_ok ? '✓' : '✗' }}
            </span>
            <span class="muted num">{{ row.node.last_probe_at }}</span>
          </span>
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
      <div class="conflict-title">待处理冲突(旧名称覆盖同名竞争落选)</div>
      <div v-for="c in conflicts" :key="c.node_key" class="conflict-row">
        <span class="conflict-name">{{ c.display_name }}</span>
        <span class="muted">{{ c.node_key }}</span>
        <span class="conflict-ops">
          <el-button link type="primary" size="small" @click="claim(c)">认领为槽位</el-button>
          <el-button link type="danger" size="small" @click="drop(c)">放弃</el-button>
        </span>
      </div>
    </div>

    <!-- 指派/换人:从节点池选节点挂到该名称 -->
    <el-dialog
      v-model="assignVisible"
      :title="`指派节点 - ${assignSlot?.name || ''}`"
      width="480px"
    >
      <el-select
        v-model="assignNodeKey"
        placeholder="搜索节点(名称/地区)"
        filterable
        class="ctl-full"
      >
        <el-option
          v-for="n in assignCandidates"
          :key="n.node_key"
          :label="`${n.display_name || n.name} · ${n.region || '—'} · ${n.source}`"
          :value="n.node_key"
        >
          <span>{{ n.display_name || n.name }}</span>
          <span class="muted option-meta">{{ n.region || '—' }} · {{ n.source }}</span>
        </el-option>
      </el-select>
      <template #footer>
        <el-button @click="assignVisible = false">取消</el-button>
        <el-button type="primary" :disabled="!assignNodeKey" @click="doAssign(false)">
          确认
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
import client from '@/api/client'
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
  // 指派候选:统一行集(机场+自建)
  nodes: UnifiedNode[]
}>()

const emit = defineEmits<{
  (e: 'changed'): void
}>()

// 可用节点排前,便于挑替补
const assignCandidates = computed(() =>
  [...props.nodes].sort((a, b) => Number(b.available) - Number(a.available))
)

const createNew = async () => {
  let name: string
  try {
    const { value } = await ElMessageBox.prompt('输入新名称(可先起名后挑节点)', '新建名称', {
      inputPattern: /\S/,
      inputErrorMessage: '名称不能为空'
    })
    name = value
  } catch {
    return
  }
  try {
    await createSlot(name)
    ElMessage.success('已创建空槽,可指派节点')
    emit('changed')
  } catch (e) {
    if (readSlotConflict(e)?.kind === 'name_taken') {
      ElMessage.error(`名称「${name}」已存在`)
    } else {
      ElMessage.error(apiErrorMessage(e, '创建失败'))
    }
  }
}

const assignVisible = ref(false)
const assignSlot = ref<NameSlot | null>(null)
const assignNodeKey = ref('')

const openAssign = (row: NameSlot) => {
  assignSlot.value = row
  assignNodeKey.value = ''
  assignVisible.value = true
}

const doAssign = async (force: boolean) => {
  if (!assignSlot.value || !assignNodeKey.value) return
  const slotName = assignSlot.value.name
  try {
    await updateSlot(slotName, { nodeKey: assignNodeKey.value, force })
    ElMessage.success('已生效,所有订阅立即使用新名称')
    assignVisible.value = false
    emit('changed')
  } catch (e) {
    const conflict = readSlotConflict(e)
    if (conflict && !force) {
      let text = ''
      if (conflict.kind === 'node_occupied') {
        text = `该节点当前挂在名称「${conflict.holder_name}」上。改挂到「${slotName}」后,「${conflict.holder_name}」将变空槽。确认?`
      } else if (conflict.kind === 'reassign') {
        text = `名称「${slotName}」当前挂在节点 ${conflict.holder_node_key} 上,确认换人?`
      }
      if (text) {
        try {
          await ElMessageBox.confirm(text, '转移确认', { type: 'warning' })
          await doAssign(true)
        } catch {
          /* 用户取消 */
        }
        return
      }
    }
    ElMessage.error(apiErrorMessage(e, '指派失败'))
  }
}

const rename = async (row: NameSlot) => {
  let newName: string
  try {
    const { value } = await ElMessageBox.prompt('新名称', `改名 - ${row.name}`, {
      inputValue: row.name,
      inputPattern: /\S/,
      inputErrorMessage: '名称不能为空'
    })
    newName = value
  } catch {
    return
  }
  if (newName === row.name) return
  try {
    await updateSlot(row.name, { newName })
    ElMessage.success('已改名,立即生效')
    emit('changed')
  } catch (e) {
    if (readSlotConflict(e)?.kind === 'name_taken') {
      ElMessage.error(`名称「${newName}」已存在`)
    } else {
      ElMessage.error(apiErrorMessage(e, '改名失败'))
    }
  }
}

const remove = async (row: NameSlot) => {
  try {
    await ElMessageBox.confirm(`删除名称「${row.name}」?挂载节点回退模板/原始名称。`, '删除名称', {
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

// 冲突认领:以新名字建槽位挂到该节点,然后清掉覆盖行残留
const claim = async (c: SlotConflictRow) => {
  let name: string
  try {
    const { value } = await ElMessageBox.prompt(
      `为节点 ${c.node_key} 起一个新名称(旧名「${c.display_name}」已被占)`,
      '认领为槽位',
      { inputValue: c.display_name, inputPattern: /\S/, inputErrorMessage: '名称不能为空' }
    )
    name = value
  } catch {
    return
  }
  try {
    await createSlot(name, c.node_key)
  } catch (e) {
    const conflict = readSlotConflict(e)
    if (conflict?.kind === 'node_occupied') {
      try {
        await ElMessageBox.confirm(
          `该节点当前挂在名称「${conflict.holder_name}」上,改挂后旧名称变空槽。确认?`,
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
  emit('changed')
}

// 放弃:清掉覆盖行(display_name 残留随行的去留规则清除,收藏保留)
const drop = async (c: SlotConflictRow) => {
  try {
    await ElMessageBox.confirm(
      `放弃节点 ${c.node_key} 的旧名称「${c.display_name}」?该名称仍被别的节点占用。`,
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
