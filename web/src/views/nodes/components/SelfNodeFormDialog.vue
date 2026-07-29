<template>
  <el-dialog v-model="visible" :title="editMode ? '编辑自建节点' : '添加自建节点'" width="600px">
    <el-form :model="form" label-width="100px">
      <el-form-item label="名称">
        <el-input v-model="form.name" placeholder="例如：自建香港" @input="nameDirty = true" />
      </el-form-item>
      <el-form-item label="协议">
        <el-select v-model="form.protocol" class="full-width">
          <el-option label="Shadowsocks (ss)" value="ss" />
          <el-option label="Trojan" value="trojan" />
          <el-option label="VMess" value="vmess" />
          <el-option label="VLESS" value="vless" />
        </el-select>
      </el-form-item>
      <el-form-item label="服务器">
        <el-input v-model="form.server" placeholder="域名或 IP" @input="onServerInput" />
      </el-form-item>
      <el-form-item label="端口">
        <el-input-number v-model="form.port" :min="1" :max="65535" />
      </el-form-item>

      <!-- 按协议动态显示字段 -->
      <el-form-item v-if="show('uuid')" label="UUID">
        <el-input v-model="form.uuid" />
      </el-form-item>
      <el-form-item v-if="show('password')" label="密码">
        <el-input v-model="form.password" />
      </el-form-item>
      <el-form-item v-if="show('cipher')" label="加密方式">
        <el-input v-model="form.cipher" placeholder="例如：aes-256-gcm" />
      </el-form-item>
      <el-form-item v-if="show('alter_id')" label="AlterID">
        <el-input-number v-model="form.alter_id" :min="0" />
      </el-form-item>
      <el-form-item v-if="show('network')" label="传输">
        <el-select v-model="form.network" class="full-width">
          <el-option label="tcp" value="tcp" />
          <el-option label="ws" value="ws" />
          <el-option label="grpc" value="grpc" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="show('grpc_service_name')" label="gRPC Service">
        <el-input v-model="form.grpc_service_name" placeholder="例如 my-grpc-service" />
      </el-form-item>
      <el-form-item v-if="show('tls')" label="TLS">
        <el-switch v-model="form.tls" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button
        type="primary"
        :loading="submitting"
        :disabled="submitting"
        @click="emit('submit')"
      >
        {{ editMode ? '保存' : '添加' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { watch, computed } from 'vue'
import client from '@/api/client'
import { fieldVisible, type SelfNodeForm } from '../self-node-utils'
import { useDebouncedSuggest } from '@/composables/useDebouncedSuggest'

const visible = defineModel<boolean>({ required: true })
const form = defineModel<SelfNodeForm>('form', { required: true })

// submitting is held by the assembly layer (set to true during async save), dialog only renders loading state
const props = defineProps<{
  editMode: boolean
  submitting: boolean
}>()

const emit = defineEmits<{
  (e: 'submit'): void
}>()

const show = (field: string) => fieldVisible(form.value, field)

// Debounced auto-suggestion: server input -> name suggestion
const nameRef = computed({
  get: () => form.value.name,
  set: (val) => {
    form.value.name = val
  }
})

const {
  isDirty: nameDirty,
  onInput: onServerInput,
  reset: resetNameSuggest
} = useDebouncedSuggest(nameRef, async () => {
  // Only suggest in create mode, when name is empty and not manually edited
  if (props.editMode || form.value.name.trim()) return null
  const server = form.value.server.trim()
  if (!server) return null

  const res = await client.get<unknown, { name: string; regionCode: string }>(
    '/self-nodes/suggest',
    { params: { server } }
  )
  return res.name || null
})

// Reset dirty flag and clear timer when dialog opens
watch(visible, (open) => {
  if (open) {
    resetNameSuggest()
  }
})
</script>

<style scoped>
.full-width {
  width: 100%;
}
</style>
