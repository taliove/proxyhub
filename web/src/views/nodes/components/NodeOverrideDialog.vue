<template>
  <el-dialog v-model="visible" title="编辑节点覆盖" width="420px">
    <el-form label-width="90px">
      <el-form-item label="原始名称">
        <span class="muted">{{ node?.name || '—' }}</span>
      </el-form-item>
      <el-form-item label="展示名称">
        <el-input v-model="displayName" placeholder="留空则用原始名称" clearable />
      </el-form-item>
      <el-form-item label="地区">
        <el-select
          v-model="region"
          placeholder="留空则用识别地区"
          clearable
          filterable
          class="ctl-full"
        >
          <el-option
            v-for="r in regions"
            :key="r.Code"
            :label="`${r.Name} (${r.Code})`"
            :value="r.Code"
          />
        </el-select>
      </el-form-item>
      <el-alert
        type="info"
        :closable="false"
        title="覆盖跨刷新保留,不被下轮机场拉取冲掉。仅可改展示名称/地区。"
      />
    </el-form>
    <template #footer>
      <el-button type="warning" plain @click="clearOverride">清除覆盖</el-button>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { Node } from '@/types'
import client from '@/api/client'
import { apiErrorMessage } from '../utils'
import type { RegionItem } from '../composables/useNodePool'

defineProps<{ regions: RegionItem[] }>()

const emit = defineEmits<{
  (e: 'saved'): void
}>()

const visible = ref(false)
const saving = ref(false)
const node = ref<Node | null>(null)
const displayName = ref('')
const region = ref('')

const open = (row: Node) => {
  node.value = row
  displayName.value = row.display_name || ''
  region.value = row.region || ''
  visible.value = true
}

const save = async () => {
  if (!node.value) return
  saving.value = true
  try {
    await client.put('/nodes/override', {
      node_key: node.value.node_key,
      display_name: displayName.value,
      region: region.value
    })
    ElMessage.success('已保存,下次生成订阅生效')
    visible.value = false
    emit('saved')
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '保存失败'))
  } finally {
    saving.value = false
  }
}

const clearOverride = async () => {
  if (!node.value) return
  try {
    await ElMessageBox.confirm('清除覆盖后将恢复机场原始展示信息,确认?', '清除覆盖', {
      type: 'warning'
    })
  } catch {
    return
  }
  try {
    await client.delete('/nodes/override', { data: { node_key: node.value.node_key } })
    ElMessage.success('已清除覆盖')
    visible.value = false
    emit('saved')
  } catch (e) {
    ElMessage.error(apiErrorMessage(e, '清除失败'))
  }
}

defineExpose({ open })
</script>

<style scoped>
.ctl-full {
  width: 100%;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
