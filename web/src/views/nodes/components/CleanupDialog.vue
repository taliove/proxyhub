<template>
  <el-dialog v-model="visible" title="清理失败节点" width="480px">
    <div class="cleanup-detect">
      <el-button size="small" type="primary" :loading="detecting" @click="detectAllThenReload">
        一键检测全部并刷新
      </el-button>
      <span class="muted">先跑真实检测,再筛出失败节点</span>
    </div>
    <div v-if="failedNodes.length === 0" class="muted">
      当前没有不可用节点。可点上方「一键检测全部」后再回到此处。
    </div>
    <template v-else>
      <el-alert type="warning" :closable="false" class="cleanup-summary">
        <template #title>
          将处理 {{ failedNodes.length }} 个不可用节点：机场 {{ airportCount }} 个、自建
          {{ selfCount }} 个
        </template>
      </el-alert>
      <el-form label-width="110px">
        <el-form-item label="机场节点">
          <el-tag type="info" size="small">批量屏蔽({{ airportCount }} 个)</el-tag>
        </el-form-item>
        <el-form-item label="自建节点处理">
          <el-radio-group v-model="selfAction">
            <el-radio value="disable">禁用(可恢复)</el-radio>
            <el-radio value="delete">删除(不可恢复)</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>
    </template>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button
        type="danger"
        :loading="running"
        :disabled="failedNodes.length === 0"
        @click="confirmCleanup"
      >
        确认处理
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Node, NodePage } from '@/types'
import client from '@/api/client'
import { apiErrorMessage, isSelfHosted } from '../utils'

// 清理失败节点闭环:机场节点批量屏蔽,自建节点禁用/删除(二次确认,不可逆感操作)。
const props = defineProps<{
  detecting: boolean
  // 由装配层注入共享的检测触发器,完成后回调刷新失败列表
  triggerDetection: (onComplete: () => void) => Promise<void>
}>()

const emit = defineEmits<{
  (e: 'done'): void
}>()

const visible = ref(false)
const running = ref(false)
const failedNodes = ref<Node[]>([])
const selfAction = ref<'disable' | 'delete'>('disable')

const airportCount = computed(() => failedNodes.value.filter((n) => !isSelfHosted(n)).length)
const selfCount = computed(() => failedNodes.value.filter((n) => isSelfHosted(n)).length)

// reloadFailedNodes 拉取全部不可用节点(排除已下架的,已不在订阅无需再处理)
const reloadFailedNodes = async () => {
  try {
    const data = await client.get<unknown, NodePage>('/nodes', {
      params: { available: 'false', stale: 'false', page_size: 1000, page: 1 }
    })
    failedNodes.value = data.nodes || []
    if (data.total && data.total > 1000) {
      ElMessage.warning(`失败节点超过 1000 个,仅加载前 1000 个`)
    }
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '加载失败节点列表失败'))
  }
}

const open = async () => {
  await reloadFailedNodes()
  selfAction.value = 'disable'
  visible.value = true
}

// 一键真实检测全部,完成后刷新失败节点列表(spec E 闭环)
const detectAllThenReload = () => {
  props.triggerDetection(async () => {
    await reloadFailedNodes()
    emit('done')
  })
}

const confirmCleanup = async () => {
  const airportKeys = failedNodes.value.filter((n) => !isSelfHosted(n)).map((n) => n.node_key)
  const selfKeys = failedNodes.value.filter((n) => isSelfHosted(n)).map((n) => n.node_key)

  const actionLabel = selfAction.value === 'delete' ? '删除' : '禁用'
  try {
    await ElMessageBox.confirm(
      `将屏蔽 ${airportKeys.length} 个机场节点,${actionLabel} ${selfKeys.length} 个自建节点。此操作${selfAction.value === 'delete' ? '不可恢复' : '可恢复'},确认?`,
      '二次确认',
      { type: 'warning', confirmButtonText: `确认${actionLabel}`, cancelButtonText: '取消' }
    )
  } catch {
    return
  }

  running.value = true
  try {
    let blocked = 0
    let processed = 0
    // 机场节点批量屏蔽
    if (airportKeys.length > 0) {
      const r = await client.post<unknown, { blocked: number }>('/nodes/cleanup', {
        node_keys: airportKeys,
        action: 'block'
      })
      blocked = r.blocked || 0
    }
    // 自建节点禁用/删除
    if (selfKeys.length > 0) {
      const r = await client.post<unknown, { disabled: number; deleted: number }>(
        '/nodes/cleanup',
        { node_keys: selfKeys, action: selfAction.value }
      )
      processed = (r.disabled || 0) + (r.deleted || 0)
    }
    ElMessage.success(`已屏蔽 ${blocked} 个机场节点,${actionLabel} ${processed} 个自建节点`)
    visible.value = false
    emit('done')
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '清理失败'))
  } finally {
    running.value = false
  }
}

defineExpose({ open })
</script>

<style scoped>
.cleanup-detect {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-3);
}
.cleanup-summary {
  margin-bottom: var(--ph-space-3);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
