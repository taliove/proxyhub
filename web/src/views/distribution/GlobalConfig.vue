<template>
  <el-form
    ref="formRef"
    :model="formData"
    :rules="rules"
    label-width="140px"
    style="max-width: 800px"
  >
    <el-alert
      type="info"
      :closable="false"
      style="margin-bottom: 16px"
      title="全局配置决定 Xray 监听端口和入站协议。修改后需重启 Xray 才能生效。"
    />

    <el-form-item label="监听端口" prop="listen_port">
      <el-input-number v-model="formData.listen_port" :min="1" :max="65535" />
    </el-form-item>

    <el-form-item label="域名" prop="domain">
      <el-input v-model="formData.domain" placeholder="example.com" />
      <span class="hint">用于 TLS 握手和路由匹配</span>
    </el-form-item>

    <el-form-item label="协议" prop="protocol">
      <el-select v-model="formData.protocol">
        <el-option label="VLESS" value="vless" />
        <el-option label="VMess" value="vmess" />
      </el-select>
    </el-form-item>

    <el-form-item label="传输方式" prop="network">
      <el-select v-model="formData.network">
        <el-option label="gRPC" value="grpc" />
        <el-option label="WebSocket" value="ws" />
      </el-select>
    </el-form-item>

    <el-form-item label="UUID" prop="uuid">
      <el-input v-model="formData.uuid" placeholder="生成或填入现有 UUID">
        <template #append>
          <el-button @click="generateUUID">生成</el-button>
        </template>
      </el-input>
    </el-form-item>

    <el-form-item label="启用 TLS">
      <el-switch v-model="formData.tls" />
    </el-form-item>

    <template v-if="formData.tls">
      <el-form-item label="证书路径" prop="cert_path">
        <el-input v-model="formData.cert_path" placeholder="/path/to/cert.pem" />
      </el-form-item>

      <el-form-item label="密钥路径" prop="key_path">
        <el-input v-model="formData.key_path" placeholder="/path/to/key.pem" />
      </el-form-item>
    </template>

    <el-form-item>
      <el-button type="primary" @click="handleSave" :loading="saving">保存配置</el-button>
      <el-button @click="handleRestart" :loading="restarting" style="margin-left: 12px">
        重启 Xray
      </el-button>
    </el-form-item>
  </el-form>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type FormInstance, type FormRules } from 'element-plus'
import { updateDistributionConfig, restartXray, type DistributionConfig } from '@/api/distribution'

const props = defineProps<{
  config: DistributionConfig
}>()

const emit = defineEmits<{
  updated: []
}>()

const formRef = ref<FormInstance>()
const formData = ref<DistributionConfig>({ ...props.config })
const saving = ref(false)
const restarting = ref(false)

const rules: FormRules = {
  listen_port: [
    { required: true, message: '请输入监听端口', trigger: 'blur' },
    { type: 'number', min: 1, max: 65535, message: '端口范围: 1-65535', trigger: 'blur' }
  ],
  domain: [
    { required: true, message: '请输入域名', trigger: 'blur' },
    {
      pattern: /^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$/,
      message: '请输入有效的域名格式',
      trigger: 'blur'
    }
  ],
  protocol: [{ required: true, message: '请选择协议', trigger: 'change' }],
  network: [{ required: true, message: '请选择传输方式', trigger: 'change' }],
  uuid: [
    { required: true, message: '请输入或生成 UUID', trigger: 'blur' },
    {
      pattern: /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
      message: '请输入有效的 UUID 格式',
      trigger: 'blur'
    }
  ],
  cert_path: [
    { required: true, message: '请输入证书路径', trigger: 'blur' }
  ],
  key_path: [
    { required: true, message: '请输入密钥路径', trigger: 'blur' }
  ]
}

const generateUUID = () => {
  formData.value.uuid = crypto.randomUUID()
}

const handleSave = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    saving.value = true
    await updateDistributionConfig(formData.value)
    ElMessage.success('配置已保存')
    emit('updated')
  } catch (error) {
    if (error instanceof Error) {
      ElMessage.error('保存失败')
    }
  } finally {
    saving.value = false
  }
}

const handleRestart = async () => {
  try {
    await ElMessageBox.confirm(
      '重启 Xray 会短暂中断流量分发服务,确定继续？',
      '确认重启',
      { type: 'warning' }
    )

    restarting.value = true
    await restartXray()
    ElMessage.success('Xray 已重启')
    emit('updated')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('重启失败')
    }
  } finally {
    restarting.value = false
  }
}

watch(() => props.config, (newConfig) => {
  formData.value = { ...newConfig }
}, { deep: true })
</script>

<style scoped>
.hint {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
</style>
