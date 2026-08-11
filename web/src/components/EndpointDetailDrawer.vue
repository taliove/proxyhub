<template>
  <el-drawer v-model="visible" :title="drawerTitle" size="720px">
    <template v-if="endpoint">
      <!-- 概况段:基础信息 + 轻管理动作;变更逻辑全部上抛订阅管理页(哑组件) -->
      <div class="drawer-block">
        <el-descriptions :column="1" border size="small">
          <el-descriptions-item label="别名">{{ endpoint.alias }}</el-descriptions-item>
          <el-descriptions-item label="订阅 URL">
            <span class="url-cell">
              <span class="url-text">{{ subscriptionUrl }}</span>
              <el-button link type="primary" @click="copyUrl">复制</el-button>
              <el-button link type="primary" @click="emit('qrcode', endpoint)">二维码</el-button>
            </span>
          </el-descriptions-item>
          <el-descriptions-item label="命名模式">
            <el-tag :type="nameModeTag(endpoint.name_mode)" size="small">
              {{ nameModeLabel(endpoint.name_mode) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="公开名称">
            <span>{{ endpoint.public_name || '未设置（客户端显示 ProxyHub）' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="节点范围">
            <el-tag :type="hasConditions(endpoint.conditions) ? 'warning' : 'info'" size="small">
              {{ hasConditions(endpoint.conditions) ? '自定义' : '全量' }}
            </el-tag>
          </el-descriptions-item>
          <!-- 精选(issue #87):概况段直接可见当前精选状态,编辑入口复用订阅管理页选择器 -->
          <el-descriptions-item label="精选节点">
            <el-tag :type="nodePicksCount(endpoint) ? 'warning' : 'info'" size="small">
              {{ nodePicksLabel(endpoint) }}
            </el-tag>
          </el-descriptions-item>
          <el-descriptions-item label="配置模板">
            <span>{{ templateDisplay }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="状态">
            <StatusDot
              :tone="endpoint.enabled ? 'success' : 'muted'"
              :label="endpoint.enabled ? '启用' : '禁用'"
              class="state-dot"
            />
            <span>{{ endpoint.enabled ? '启用' : '禁用' }}</span>
          </el-descriptions-item>
          <el-descriptions-item label="虚拟状态节点">
            <el-switch
              :model-value="endpoint.status_node_enabled"
              :loading="statusNodeSaving"
              @change="onStatusNodeChange"
            />
            <span class="status-node-hint">在订阅第一位注入节点状态摘要</span>
          </el-descriptions-item>
          <el-descriptions-item label="节点来源">
            <el-radio-group
              :model-value="endpoint.slot_mode ? 'slots' : 'pool'"
              size="small"
              :disabled="slotModeSaving"
              @change="onSlotModeChange"
            >
              <el-radio-button label="pool">池模式</el-radio-button>
              <el-radio-button label="slots">槽位模式</el-radio-button>
            </el-radio-group>
            <span class="status-node-hint"> 槽位模式只下发名称槽位挂载的节点，名字即槽位名 </span>
          </el-descriptions-item>
        </el-descriptions>
        <div class="drawer-actions">
          <el-button size="small" @click="emit('toggle', endpoint)">
            {{ endpoint.enabled ? '禁用' : '启用' }}
          </el-button>
          <el-button size="small" @click="emit('name-config', endpoint)">命名设置</el-button>
          <el-button size="small" @click="emit('public-name', endpoint)">公开名称</el-button>
          <el-tooltip content="槽位模式下由名称槽位决定" :disabled="!endpoint.slot_mode">
            <span>
              <el-button
                size="small"
                :disabled="endpoint.slot_mode"
                @click="emit('conditions', endpoint)"
              >
                节点范围
              </el-button>
            </span>
          </el-tooltip>
          <el-tooltip content="槽位模式下由名称槽位决定" :disabled="!endpoint.slot_mode">
            <span>
              <el-button
                size="small"
                :disabled="endpoint.slot_mode"
                @click="emit('picks', endpoint)"
              >
                精选节点
              </el-button>
            </span>
          </el-tooltip>
          <el-button size="small" @click="openTemplateConfig">配置模板</el-button>
          <el-button size="small" type="danger" @click="emit('delete', endpoint)">删除</el-button>
        </div>
      </div>

      <!-- 下发节点清单段:与 /sub 同一生成链的所见即所得(吸收原预览对话框);
           订阅原文折叠展示,Clash/V2Ray 切换重拉。 -->
      <div class="drawer-block">
        <div class="drawer-section-title">下发节点清单</div>
        <div class="preview-toolbar">
          <el-radio-group v-model="format" size="small" @change="loadPreview">
            <el-radio-button label="clash">Clash</el-radio-button>
            <el-radio-button label="v2ray">V2Ray</el-radio-button>
          </el-radio-group>
          <span class="preview-hint">
            共 {{ preview.count }} 个节点（已应用节点范围条件，与终端拉取到的完全一致）
          </span>
        </div>
        <el-table
          v-loading="previewLoading"
          :data="preview.nodes"
          size="small"
          border
          max-height="300"
        >
          <el-table-column label="名称" min-width="140" show-overflow-tooltip>
            <template #default="{ row }">{{ row.display_name || row.name }}</template>
          </el-table-column>
          <el-table-column label="地区" width="72">
            <template #default="{ row }">{{ regionDisplay(row.region) }}</template>
          </el-table-column>
          <el-table-column label="延迟" width="80">
            <template #default="{ row }">
              <span class="num">{{ nodeLatencyText(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="可用" width="80">
            <template #default="{ row }">
              <el-tag :type="row.available ? 'success' : 'info'" size="small">
                {{ row.available ? '可用' : '不可用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="source" label="来源" width="110" show-overflow-tooltip />
          <template #empty>
            <span class="muted">当前节点范围条件下无可下发节点。</span>
          </template>
        </el-table>
        <el-collapse class="raw-collapse">
          <el-collapse-item title="订阅原文" name="raw">
            <el-input
              v-model="preview.content"
              type="textarea"
              :rows="8"
              readonly
              placeholder="(无节点内容)"
            />
          </el-collapse-item>
        </el-collapse>
      </div>

      <!-- 订阅测试段:关闭抽屉时卸载(v-if),内部轮询随卸载停止;打开不自动测 -->
      <div class="drawer-block">
        <EndpointTestSection v-if="visible" :endpoint="endpoint" />
      </div>

      <!-- 地域白名单段:三档开关 + 国家多选 + 省份区(降级警告形态) -->
      <div class="drawer-block">
        <div class="drawer-section-title">地域白名单</div>
        <EndpointGeoConfigSection :endpoint="endpoint" @saved="emit('template-changed')" />
      </div>

      <!-- 拉取统计段:真实客户端拉取明细(吸收原统计抽屉) -->
      <div class="drawer-block">
        <div class="drawer-section-title">拉取统计</div>
        <IPStatsTable :endpoint-id="endpoint.id" />
      </div>
    </template>

    <!-- 配置模板对话框:挂在抽屉外层,不受抽屉关闭影响 -->
    <el-dialog v-model="templateConfigVisible" title="配置模板" width="460px" append-to-body>
      <el-form label-width="80px">
        <el-form-item label="配置模板">
          <el-select
            v-model="templateConfigForm.template_name"
            placeholder="跟随默认模板"
            clearable
            class="full-width"
          >
            <el-option
              v-for="tpl in templates"
              :key="tpl.name"
              :label="tpl.name"
              :value="tpl.name"
            />
          </el-select>
          <div class="cfg-hint">
            留空则跟随用户默认模板（四级回退：订阅地址 → 用户默认 → 超管全局 → 内嵌默认）
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="templateConfigVisible = false">取消</el-button>
        <el-button type="primary" @click="saveTemplateConfig">保存</el-button>
      </template>
    </el-dialog>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import type { Endpoint } from '@/types'
import client from '@/api/client'
import { updateEndpointTemplate } from '@/api/endpoints'
import StatusDot from '@/components/StatusDot.vue'
import IPStatsTable from '@/components/IPStatsTable.vue'
import EndpointTestSection from '@/components/EndpointTestSection.vue'
import EndpointGeoConfigSection from '@/components/EndpointGeoConfigSection.vue'
import { hasConditions } from '@/utils/conditions'
import { nodePicksCount, nodePicksLabel } from '@/components/endpoint-nodepicks-utils'
import { nameModeLabel, nameModeTag } from '@/utils/namemode'
import { useTemplateList } from '@/composables/useTemplateList'
import { useEndpointToggles } from '@/composables/useEndpointToggles'
import { regionDisplay } from '@/views/nodes/nodecells'
import { copyText } from '@/utils/clipboard'

// 预览节点(与后端 toNodeViews 输出对齐,只取本段所需字段)
interface PreviewNode {
  name: string
  display_name?: string
  region?: string
  latency?: number
  source?: string
  available: boolean
}

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  endpoint: Endpoint | null
  // 完整订阅 URL(由父级按 origin+path+token 拼好;二维码与复制共用)
  subscriptionUrl: string
}>()

const emit = defineEmits<{
  (e: 'toggle', endpoint: Endpoint): void
  (e: 'name-config', endpoint: Endpoint): void
  (e: 'public-name', endpoint: Endpoint): void
  (e: 'conditions', endpoint: Endpoint): void
  (e: 'picks', endpoint: Endpoint): void
  (e: 'delete', endpoint: Endpoint): void
  (e: 'qrcode', endpoint: Endpoint): void
  (e: 'template-changed'): void
  (e: 'status-node-changed'): void
}>()

const drawerTitle = computed(() =>
  props.endpoint ? `订阅详情 - ${props.endpoint.alias}` : '订阅详情'
)

// ---- 模板配置:显示当前模板,提供改绑入口 ----
const templateDisplay = computed(() => {
  if (!props.endpoint) return ''
  return props.endpoint.template_name || '默认模板'
})

const templateConfigVisible = ref(false)
const templateConfigForm = ref({ template_name: '' })
const { templates, loadTemplates } = useTemplateList()

const openTemplateConfig = async () => {
  if (!props.endpoint) return
  await loadTemplates()
  templateConfigForm.value.template_name = props.endpoint.template_name || ''
  templateConfigVisible.value = true
}

// ---- 直改直存开关(虚拟状态节点/槽位模式):逻辑抽 useEndpointToggles(400 行门禁) ----
// 两个开关变更统一上抛 status-node-changed(父页只负责重取列表)
const { statusNodeSaving, onStatusNodeChange, slotModeSaving, onSlotModeChange } =
  useEndpointToggles(
    computed(() => props.endpoint),
    () => emit('status-node-changed')
  )

const saveTemplateConfig = async () => {
  if (!props.endpoint) return
  try {
    await updateEndpointTemplate(props.endpoint.id, templateConfigForm.value.template_name)
    ElMessage.success('模板已更新')
    templateConfigVisible.value = false
    emit('template-changed')
  } catch (err) {
    ElMessage.error(`更新失败：${err instanceof Error ? err.message : String(err)}`)
  }
}

// ---- 下发节点清单段:打开抽屉按当前格式拉一次;切换格式重拉。失败降级空态,不阻塞抽屉。 ----
const format = ref<'clash' | 'v2ray'>('clash')
const preview = ref<{ count: number; content: string; nodes: PreviewNode[] }>({
  count: 0,
  content: '',
  nodes: []
})
const previewLoading = ref(false)

const loadPreview = async () => {
  if (!props.endpoint) return
  previewLoading.value = true
  try {
    preview.value = await client.get(
      `/endpoints/${props.endpoint.id}/preview?format=${format.value}`
    )
  } catch {
    preview.value = { count: 0, content: '', nodes: [] }
  } finally {
    previewLoading.value = false
  }
}

const nodeLatencyText = (n: PreviewNode): string =>
  n.latency && n.latency > 0 ? `${n.latency}ms` : '—'

// 打开抽屉/切换端点(父级在启停/命名/范围变更后刷新端点对象)时重拉清单;
// 关闭时清空,避免下次闪现旧端点数据。
watch(
  () => [visible.value, props.endpoint] as const,
  ([open, endpoint]) => {
    if (open && endpoint) {
      format.value = 'clash'
      loadPreview()
    } else if (!open) {
      preview.value = { count: 0, content: '', nodes: [] }
    }
  },
  { immediate: true }
)

const copyUrl = async () => {
  try {
    await copyText(props.subscriptionUrl)
    ElMessage.success('订阅 URL 已复制到剪贴板')
  } catch (err) {
    ElMessage.error(`复制失败：${err instanceof Error ? err.message : String(err)}`)
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
  margin-top: var(--ph-space-3);
}
.url-cell {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.url-text {
  word-break: break-all;
}
.status-node-hint {
  margin-left: var(--ph-space-2);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.state-dot {
  margin-right: var(--ph-space-1);
}
.preview-toolbar {
  display: flex;
  align-items: center;
  margin-bottom: var(--ph-space-3);
}
.preview-hint {
  margin-left: var(--ph-space-3);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.raw-collapse {
  margin-top: var(--ph-space-3);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
.cfg-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
  margin-top: var(--ph-space-1);
}
.full-width {
  width: 100%;
}
</style>
