<template>
  <!-- 槽位卡片墙(spec-slot-observability-cards):按状态分组、异常排前;
       每卡 = 名称 + 状态点 + 挂载节点 + 24h 网格 + 连通率 + 轻操作。
       删除留表格视图(危险红纪律:低曝光),卡片只放指派/改名。 -->
  <div class="slot-cards">
    <div v-for="g in groups" :key="g.status" class="slot-group">
      <div class="slot-group__head">
        <StatusDot :tone="meta(g.status).tone" :label="meta(g.status).label" />
        <span class="slot-group__label">{{ meta(g.status).label }}</span>
        <span class="slot-group__count num">{{ g.rows.length }}</span>
      </div>
      <div class="slot-group__grid">
        <div v-for="row in g.rows" :key="row.id" class="slot-card">
          <div class="slot-card__head">
            <span class="slot-card__name" :title="row.name">{{ row.display || row.name }}</span>
            <span class="slot-card__status">{{ meta(statusOf(row)).label }}</span>
          </div>
          <!-- 模板原文不展示(用户反馈无意义);渲染名回退模板名由 name 行承担 -->
          <div class="slot-card__node">
            <template v-if="row.empty">待指派节点</template>
            <template v-else-if="row.node?.missing">节点已消失</template>
            <template v-else-if="row.node">{{ row.node.name }} · {{ row.node.source }}</template>
          </div>
          <div class="slot-card__probe">
            <ProbeGrid v-if="row.probe_grid" :grid="row.probe_grid" :stats="row.probe_stats" />
            <template v-else-if="!monitorEnabled">
              <el-link type="primary" @click="goAlert">监控未开启，去告警设置</el-link>
            </template>
            <span v-else class="muted">—</span>
            <span v-if="uptime24h(row.probe_grid) !== null" class="slot-card__uptime num">
              {{ uptime24h(row.probe_grid) }}%
            </span>
          </div>
          <div class="slot-card__ops">
            <el-button link type="primary" size="small" @click="emit('assign', row)">
              {{ row.empty || row.node?.missing || row.node?.stale ? '指派节点' : '换节点' }}
            </el-button>
            <el-button link size="small" @click="emit('rename', row)">改名</el-button>
          </div>
        </div>
      </div>
    </div>
    <el-empty v-if="groups.length === 0" description="无匹配槽位" :image-size="60" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import StatusDot from '@/components/StatusDot.vue'
import ProbeGrid from './ProbeGrid.vue'
import {
  statusOf,
  uptime24h,
  SLOT_GROUP_ORDER,
  SLOT_STATUS_META,
  type SlotStatus
} from './slotstatus'
import { useAuthStore } from '@/stores/auth'
import type { NameSlot } from '@/api/slots'

const props = defineProps<{
  slots: NameSlot[]
  monitorEnabled: boolean
}>()

const emit = defineEmits<{
  (e: 'assign', row: NameSlot): void
  (e: 'rename', row: NameSlot): void
}>()

const router = useRouter()
const authStore = useAuthStore()

const meta = (s: SlotStatus) => SLOT_STATUS_META[s]

const groups = computed(() =>
  SLOT_GROUP_ORDER.map((status) => ({
    status,
    rows: props.slots.filter((s) => statusOf(s) === status)
  })).filter((g) => g.rows.length > 0)
)

// 一键抵达告警配置(快修批⑥深链);tab 超管专属,普通用户落到其首个可见 tab
const goAlert = () => {
  if (authStore.isSuperAdmin) router.push({ name: 'Settings', query: { tab: 'alert' } })
  else router.push({ name: 'Settings' })
}
</script>

<style scoped>
.slot-cards {
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-4);
}
.slot-group__head {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-2);
}
.slot-group__label {
  font-weight: 600;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-primary);
}
.slot-group__count {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.slot-group__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: var(--ph-space-3);
}
.slot-card {
  border: 1px solid var(--ph-border-light);
  border-radius: var(--ph-radius-lg);
  background: var(--ph-bg-surface);
  padding: var(--ph-space-3);
  display: flex;
  flex-direction: column;
  gap: var(--ph-space-2);
  transition: box-shadow var(--ph-transition);
}
.slot-card:hover {
  box-shadow: var(--ph-shadow-md, var(--ph-shadow-sm));
}
.slot-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ph-space-2);
}
.slot-card__name {
  font-weight: 600;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.slot-card__status {
  flex-shrink: 0;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.slot-card__node {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.slot-card__probe {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: var(--ph-space-2);
  min-height: 16px;
}
.slot-card__uptime {
  flex-shrink: 0;
  margin-left: auto;
  font-size: var(--ph-text-xs);
  font-weight: 600;
  color: var(--ph-text-regular);
}
.slot-card__ops {
  display: flex;
  gap: var(--ph-space-1);
  border-top: 1px solid var(--ph-border-light);
  padding-top: var(--ph-space-2);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
</style>
