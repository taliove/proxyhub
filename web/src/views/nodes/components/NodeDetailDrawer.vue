<template>
  <el-drawer v-model="visible" title="节点详情" size="640px">
    <template v-if="node">
      <el-descriptions :column="2" border size="small" class="drawer-block">
        <el-descriptions-item label="原始名称">{{ node.name }}</el-descriptions-item>
        <el-descriptions-item label="标准名称">{{ node.display_name || '—' }}</el-descriptions-item>
        <el-descriptions-item label="服务器">{{ node.server }}</el-descriptions-item>
        <el-descriptions-item label="端口">
          <span class="num">{{ node.port }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="协议">{{ node.type }}</el-descriptions-item>
        <el-descriptions-item label="传输">{{ node.network || 'tcp' }}</el-descriptions-item>
        <el-descriptions-item label="TLS">{{ node.tls ? '是' : '否' }}</el-descriptions-item>
        <el-descriptions-item label="SNI">{{ node.sni || '—' }}</el-descriptions-item>
        <el-descriptions-item label="地区">{{ node.region || '—' }}</el-descriptions-item>
        <el-descriptions-item label="来源">{{ node.source }}</el-descriptions-item>
        <el-descriptions-item label="延迟">
          <span class="num">{{ latencyText(node) }}</span>
        </el-descriptions-item>
      </el-descriptions>

      <!-- 可用性诊断:回答"这个节点为什么可用/不可用、进没进订阅"(ticket 0016);
           「失败原因」行由 ticket 0017 落地:检测写回链路结构化记录分类+短详情 -->
      <div class="drawer-block">
        <div class="drawer-section-title">可用性诊断</div>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="可用状态">
            <el-tag :type="node.available ? 'success' : 'danger'" size="small">
              {{ node.available ? '可用' : '不可用' }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="判定来源">
            {{ availabilitySourceText(node) }}
          </el-descriptions-item>
          <el-descriptions-item label="最近检测时间">
            <span v-if="node.detection_last_check">{{
              formatTime(node.detection_last_check)
            }}</span>
            <span v-else class="muted">—</span>
          </el-descriptions-item>
          <el-descriptions-item label="失败原因">
            <!-- 可用:原因已随成功清空;从未检测:引导文案;失败:分类 + 短详情(ticket 0017) -->
            <span v-if="node.available" class="muted">—</span>
            <span v-else-if="node.availability_source === 'never'" class="muted">
              从未检测;需真实检测后才会进入订阅
            </span>
            <template v-else-if="node.detection_fail_reason">
              <span class="error-text">{{ failReasonText(node.detection_fail_reason) }}</span>
              <span v-if="node.detection_fail_detail" class="muted fail-detail">
                {{ node.detection_fail_detail }}
              </span>
            </template>
            <span v-else class="muted">—</span>
          </el-descriptions-item>
        </el-descriptions>
        <div class="diag-hint">{{ subscriptionHint(node) }}</div>
      </div>

      <!-- 完整协议参数(排障用):plugin/plugin_opts 等已落库字段透出(ticket 0016);
           uuid/password 属凭证,后端不透出 -->
      <div class="drawer-block">
        <div class="drawer-section-title">协议参数</div>
        <el-descriptions :column="2" border size="small">
          <el-descriptions-item label="加密方式">{{ node.cipher || '—' }}</el-descriptions-item>
          <el-descriptions-item label="AlterID">
            <span class="num">{{ node.alter_id ?? '—' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="gRPC Service">{{
            node.grpc_service_name || '—'
          }}</el-descriptions-item>
          <el-descriptions-item label="跳过证书校验">{{
            node.insecure ? '是' : '否'
          }}</el-descriptions-item>
          <el-descriptions-item label="插件" :span="2">{{
            node.plugin || '—'
          }}</el-descriptions-item>
          <el-descriptions-item label="插件参数" :span="2">
            <span class="plugin-opts">{{ node.plugin_opts || '—' }}</span>
          </el-descriptions-item>
        </el-descriptions>
      </div>

      <div class="drawer-block">
        <div class="drawer-section-title">解锁检测结果</div>
        <el-table v-if="rows.length > 0" :data="rows" size="small" border>
          <el-table-column prop="target" label="检测目标" width="140" />
          <el-table-column label="状态" width="150">
            <template #default="{ row: r }">
              <span class="status-cell">
                <!-- 通用探测保持既有 通过/失败;解锁目标用三档语义色 + 文案 -->
                <el-tag
                  v-if="isGenericVariant(r.display.variant)"
                  :type="r.display.tagType"
                  size="small"
                >
                  {{ r.result.available ? '通过' : '失败' }}
                </el-tag>
                <el-tag v-else :type="r.display.tagType" size="small">{{ r.display.label }}</el-tag>
                <el-tag
                  v-if="r.region"
                  size="small"
                  type="info"
                  effect="plain"
                  class="region-badge"
                >
                  {{ r.region }}
                </el-tag>
              </span>
            </template>
          </el-table-column>
          <el-table-column label="延迟" width="90">
            <template #default="{ row: r }">
              <span v-if="r.result.available" class="num">{{ r.result.latency }}ms</span>
              <span v-else class="muted">—</span>
            </template>
          </el-table-column>
          <el-table-column label="失败原因" min-width="200">
            <template #default="{ row: r }">
              <span v-if="r.result.available" class="muted">—</span>
              <span v-else-if="r.display.variant === 'error'" class="muted">
                {{ r.result.error || '检测失败' }}
              </span>
              <span v-else class="error-text">{{ r.result.error || '不可用' }}</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-else class="muted">该节点暂无检测记录,可点下方「检测此节点」运行检测。</div>
      </div>

      <div v-if="node.bandwidth_down_mbps || node.bandwidth_up_mbps" class="drawer-block bw-detail">
        <span class="bw-label">带宽测试:</span>
        <el-tag size="small" type="success" class="num">
          下行 {{ (node.bandwidth_down_mbps || 0).toFixed(1) }} Mbps
        </el-tag>
        <el-tag size="small" type="success" class="num">
          上行 {{ (node.bandwidth_up_mbps || 0).toFixed(1) }} Mbps
        </el-tag>
      </div>

      <!-- 体检历史时间线:历次深度体检,点开看完整三段报告卡;空历史给引导态 -->
      <div class="drawer-block">
        <ExamHistoryTimeline
          :entries="examEntries"
          :loading="examLoading"
          :node-name="node.display_name || node.name"
          @exam="emit('exam', node)"
        />
      </div>

      <!-- 抽屉内只允许轻量操作:针对当前节点跑一次解锁检测 / 跳本机实测(预填标注) -->
      <div class="drawer-actions">
        <el-button type="primary" size="small" :disabled="detecting" @click="emit('detect', node)">
          检测此节点
        </el-button>
        <el-button size="small" @click="emit('speedtest', node)">本机实测</el-button>
        <el-button v-if="canShowNodeQR(node)" size="small" @click="showNodeQR(node)">
          节点二维码
        </el-button>
      </div>
    </template>

    <!-- 节点二维码对话框 -->
    <el-dialog v-model="qrVisible" title="节点分享二维码" width="400px">
      <div v-if="qrLoading" class="qr-loading">生成二维码中...</div>
      <div v-else-if="qrError" class="qr-error">
        <el-alert type="error" :closable="false">{{ qrError }}</el-alert>
      </div>
      <div v-else class="qr-content">
        <img v-if="qrDataUrl" :src="qrDataUrl" alt="节点分享二维码" class="qr-image" />
        <div class="qr-hint">使用客户端扫码即可导入此节点</div>
      </div>
      <template #footer>
        <el-button type="primary" @click="qrVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ExamHistoryEntry, Node } from '@/types'
import { isGenericVariant, unlockDisplayRows } from '../unlock'
import { latencyText } from '../nodecells'
import { availabilitySourceText, failReasonText, formatTime, subscriptionHint } from '../utils'
import { fetchExamHistory } from '@/api/exam'
import ExamHistoryTimeline from '@/components/exam/ExamHistoryTimeline.vue'
import { generateQRCode } from '@/composables/useQRCode'
import { canGenerateShareLink, getNodeShareLink } from '@/composables/useNodeShare'

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  node: Node | null
  detecting: boolean
}>()

const emit = defineEmits<{
  (e: 'detect', node: Node): void
  (e: 'exam', node: Node): void
  // 跳本机实测页并预填该节点标注(ticket 0034)
  (e: 'speedtest', node: Node): void
}>()

const rows = computed(() => (props.node ? unlockDisplayRows(props.node) : []))

// 体检历史:抽屉打开且有节点时按需拉取(每节点最多 50 条,一次拉全,时间线内分批渲染)。
const examEntries = ref<ExamHistoryEntry[]>([])
const examLoading = ref(false)

const loadExamHistory = async (nodeKey: string) => {
  examLoading.value = true
  examEntries.value = []
  try {
    examEntries.value = await fetchExamHistory({ node_key: nodeKey })
  } catch {
    examEntries.value = [] // 历史查询失败不阻塞抽屉,静默降级为空态
  } finally {
    examLoading.value = false
  }
}

// 打开抽屉 / 切换节点时刷新历史;关闭时清空,避免下次闪现旧节点数据。
watch(
  () => [visible.value, props.node?.node_key] as const,
  ([open, key]) => {
    if (open && key) loadExamHistory(key)
    else if (!open) examEntries.value = []
  },
  { immediate: true }
)

// 节点二维码
const qrVisible = ref(false)
const qrLoading = ref(false)
const qrError = ref('')
const qrDataUrl = ref('')

const canShowNodeQR = (node: Node): boolean => {
  return canGenerateShareLink(node)
}

const showNodeQR = async (node: Node) => {
  qrVisible.value = true
  qrLoading.value = true
  qrError.value = ''
  qrDataUrl.value = ''

  try {
    const uri = await getNodeShareLink(node)
    qrDataUrl.value = await generateQRCode(uri)
  } catch (err) {
    qrError.value = err instanceof Error ? err.message : String(err)
  } finally {
    qrLoading.value = false
  }
}
</script>

<style scoped>
.drawer-block {
  margin-bottom: var(--ph-space-5);
}
.drawer-section-title {
  font-weight: 600;
  margin-bottom: var(--ph-space-2);
}
.drawer-actions {
  margin-top: var(--ph-space-4);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.diag-hint {
  margin-top: var(--ph-space-2);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.plugin-opts {
  word-break: break-all;
}
.num {
  font-variant-numeric: tabular-nums;
}
.error-text {
  color: var(--ph-danger);
}
.fail-detail {
  display: block;
  word-break: break-all;
}
.status-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
}
.region-badge {
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.04em;
}
.bw-detail {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.bw-label {
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.qr-content {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: var(--ph-space-4) 0;
}
.qr-image {
  max-width: 100%;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-lg);
}
.qr-hint {
  margin-top: var(--ph-space-3);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.qr-loading,
.qr-error {
  text-align: center;
  padding: var(--ph-space-4) 0;
}
</style>
