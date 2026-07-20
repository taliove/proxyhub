<template>
  <el-dialog v-model="visible" :title="editMode ? '编辑分发节点' : '新建分发节点'" width="600px">
    <el-form :model="form" label-width="120px">
      <el-form-item label="名称" required>
        <el-input v-model="form.name" placeholder="例如:香港分发" />
      </el-form-item>
      <el-form-item label="分发路径" required>
        <el-input v-model="form.path" placeholder="例如:/hk-dist">
          <template #prepend>
            <el-button :disabled="!form.name" @click="autoPath">自动生成</el-button>
          </template>
        </el-input>
        <div class="hint">路径必须以 / 开头,用于订阅分发</div>
      </el-form-item>
      <el-form-item label="负载均衡策略" required>
        <el-select v-model="form.lb_strategy" class="full-width">
          <el-option label="随机 (random)" value="random" />
          <el-option label="轮询 (round_robin)" value="round_robin" />
          <el-option label="最少连接 (least_conn)" value="least_conn" />
        </el-select>
      </el-form-item>
      <el-form-item label="上游节点" required>
        <el-select
          v-model="form.upstream_node_keys"
          multiple
          filterable
          placeholder="选择上游节点"
          class="full-width"
          :loading="loadingNodes"
        >
          <el-option-group v-for="group in groupedNodes" :key="group.label" :label="group.label">
            <el-option
              v-for="node in group.nodes"
              :key="node.node_key"
              :label="`${node.display_name || node.name} (${node.region})`"
              :value="node.node_key"
              :disabled="!node.available"
            >
              <span>{{ node.display_name || node.name }}</span>
              <span class="opt-meta">{{ node.region }} · {{ node.type }}</span>
            </el-option>
          </el-option-group>
        </el-select>
        <div class="hint">已选择 {{ form.upstream_node_keys.length }} 个节点</div>
      </el-form-item>
      <el-form-item label="启用">
        <el-switch v-model="form.enabled" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :loading="submitting" :disabled="submitting" @click="onSubmit">
        {{ editMode ? '保存' : '创建' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import type { Node } from '@/types'
import type { CreateDistributionNodeRequest } from '@/api/distribution-nodes'
import { pathFromName } from '../distribution-node-utils'

const visible = defineModel<boolean>({ required: true })
const form = defineModel<CreateDistributionNodeRequest>('form', { required: true })

defineProps<{
  editMode: boolean
  submitting: boolean
  loadingNodes: boolean
  groupedNodes: { label: string; nodes: Node[] }[]
}>()

const emit = defineEmits<{
  (e: 'submit'): void
}>()

const autoPath = () => {
  if (!form.value.name) {
    ElMessage.warning('请先输入名称')
    return
  }
  form.value.path = pathFromName(form.value.name)
}

// 提交前做与原实现一致的字段校验
const onSubmit = () => {
  if (!form.value.name.trim()) {
    ElMessage.warning('名称不能为空')
    return
  }
  if (!form.value.path.trim()) {
    ElMessage.warning('分发路径不能为空')
    return
  }
  if (!form.value.path.startsWith('/')) {
    ElMessage.warning('分发路径必须以 / 开头')
    return
  }
  if (form.value.upstream_node_keys.length === 0) {
    ElMessage.warning('请至少选择一个上游节点')
    return
  }
  emit('submit')
}
</script>

<style scoped>
.full-width {
  width: 100%;
}
.hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  margin-top: var(--ph-space-1);
}
.opt-meta {
  color: var(--ph-text-secondary);
  margin-left: var(--ph-space-2);
}
</style>
