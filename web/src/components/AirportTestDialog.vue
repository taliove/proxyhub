<template>
  <el-dialog
    v-model="visible"
    :title="`机场测试 - ${airport?.name || ''}`"
    width="600px"
    @close="handleClose"
  >
    <!-- Diagnostics phase -->
    <div v-if="phase === 'diagnosing'" class="test-phase">
      <div class="phase-loading">
        <el-icon class="is-loading"><Loading /></el-icon>
        <span>正在诊断订阅...</span>
      </div>
    </div>

    <!-- Diagnostics results -->
    <div v-else-if="phase === 'diagnostic-done'" class="test-phase">
      <h4>📊 诊断结果</h4>
      <el-descriptions :column="2" border>
        <el-descriptions-item label="HTTP 状态">
          <el-tag :type="diagnosticResult.http_status === 200 ? 'success' : 'danger'">
            {{ diagnosticResult.http_status }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="耗时">
          {{ diagnosticResult.duration_ms }} ms
        </el-descriptions-item>
        <el-descriptions-item label="解析成功">
          {{ diagnosticResult.node_count }} 节点
        </el-descriptions-item>
        <el-descriptions-item label="解析失败">
          <el-tag v-if="diagnosticResult.parse_failures > 0" type="warning">
            {{ diagnosticResult.parse_failures }} 行
          </el-tag>
          <span v-else>0</span>
        </el-descriptions-item>
      </el-descriptions>

      <div v-if="diagnosticResult.protocol_counts" class="protocol-counts">
        <h5>协议分布</h5>
        <el-tag
          v-for="(count, protocol) in diagnosticResult.protocol_counts"
          :key="protocol"
          class="protocol-tag"
        >
          {{ protocol }}: {{ count }}
        </el-tag>
      </div>

      <!-- Future phases placeholder -->
      <div class="future-phases">
        <el-divider />
        <el-alert type="info" :closable="false" show-icon>
          <template #title> 抽样检活与综合评分功能即将支持 </template>
        </el-alert>
      </div>
    </div>

    <!-- Error state -->
    <div v-else-if="phase === 'failed'" class="test-phase">
      <el-alert type="error" :closable="false" show-icon>
        <template #title>测试失败</template>
        {{ errorMessage }}
      </el-alert>
    </div>

    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { Loading } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import type { Airport } from '@/types'
import { runAirportTest, type DiagnosticResult } from '@/composables/useAirportTest'

interface Props {
  modelValue: boolean
  airport: Airport | null
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const visible = ref(false)
const phase = ref<'diagnosing' | 'diagnostic-done' | 'failed'>('diagnosing')
const diagnosticResult = ref<DiagnosticResult>({
  http_status: 0,
  duration_ms: 0,
  node_count: 0,
  protocol_counts: {},
  parse_failures: 0
})
const errorMessage = ref('')

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
    if (val && props.airport) {
      startTest()
    }
  }
)

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const startTest = async () => {
  if (!props.airport) return

  phase.value = 'diagnosing'
  errorMessage.value = ''

  try {
    const result = await runAirportTest(props.airport.id)

    if (result.status === 'failed') {
      phase.value = 'failed'
      errorMessage.value = result.error_message || '未知错误'
      return
    }

    const dims = JSON.parse(result.dimensions_json) as DiagnosticResult
    diagnosticResult.value = dims
    phase.value = 'diagnostic-done'
  } catch (error: any) {
    phase.value = 'failed'
    errorMessage.value = error.response?.data?.error || error.message || '请求失败'
    ElMessage.error('测试执行失败')
  }
}

const handleClose = () => {
  // Reset state on close
  phase.value = 'diagnosing'
  diagnosticResult.value = {
    http_status: 0,
    duration_ms: 0,
    node_count: 0,
    protocol_counts: {},
    parse_failures: 0
  }
  errorMessage.value = ''
}
</script>

<style scoped>
.test-phase {
  min-height: 200px;
}

.phase-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 40px 0;
  font-size: 16px;
  color: var(--el-text-color-secondary);
}

.phase-loading .el-icon {
  font-size: 24px;
}

.protocol-counts {
  margin-top: 20px;
}

.protocol-counts h5 {
  margin-bottom: 12px;
  font-size: 14px;
  color: var(--el-text-color-regular);
}

.protocol-tag {
  margin-right: 8px;
}

.future-phases {
  margin-top: 20px;
}
</style>
