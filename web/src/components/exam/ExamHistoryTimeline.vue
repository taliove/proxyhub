<template>
  <div class="exam-timeline">
    <div class="exam-timeline-head">
      <span class="drawer-section-title">体检历史</span>
      <span v-if="items.length" class="muted">共 {{ items.length }} 次</span>
    </div>

    <div v-if="loading" class="exam-timeline-hint">加载中…</div>

    <!-- 空历史引导态:引导用户去跑一次深度体检 -->
    <div v-else-if="items.length === 0" class="exam-timeline-empty">
      <div class="muted">尚未体检。深度体检会采样稳定性、多地域测速与流媒体/AI 解锁。</div>
      <el-button type="primary" size="small" @click="emit('exam')">去跑一次深度体检</el-button>
    </div>

    <template v-else>
      <ul class="exam-timeline-list">
        <li v-for="item in visibleItems" :key="item.id" class="exam-timeline-item">
          <button
            type="button"
            class="exam-timeline-row"
            :class="{ 'is-open': item.id === selectedId }"
            @click="toggle(item.id)"
          >
            <span class="exam-timeline-time">{{ item.relative }}</span>
            <el-tag v-if="item.scoreLevel" :type="tagType(item.scoreLevel)" size="small">
              稳定性 {{ item.score }}
            </el-tag>
            <el-tag v-else size="small" type="info">稳定性 —</el-tag>
            <span v-if="item.unlockSummary" class="exam-timeline-unlock">{{
              item.unlockSummary
            }}</span>
            <button
              type="button"
              class="exam-timeline-share"
              :title="'分享体检'"
              @click.stop="openShare(item.id)"
            >
              <el-icon><Share /></el-icon>
            </button>
            <el-icon class="exam-timeline-caret" :class="{ 'is-open': item.id === selectedId }">
              <ArrowRight />
            </el-icon>
          </button>

          <!-- 点开渲染完整三段报告卡(复用实时体检同款段组件) -->
          <ExamReportCard
            v-if="item.id === selectedId && selectedReport"
            :report="selectedReport"
            :node-name="nodeName"
            :exam-time="item.createdAt"
            class="exam-timeline-report"
          />
        </li>
      </ul>

      <!-- 按需加载:历史上限 50 条,首屏只渲染一批,其余点开加载 -->
      <div v-if="hasMore" class="exam-timeline-more">
        <el-button link type="primary" size="small" @click="showMore">
          加载更多({{ items.length - visibleCount }})
        </el-button>
      </div>
    </template>

    <!-- 分享对话框:点击任一时间线条目的分享按钮即可唤起 -->
    <ExamShareDialog
      v-model:visible="shareVisible"
      :report="shareReport"
      :node-name="nodeName"
      :exam-time="shareExamTime"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ArrowRight, Share } from '@element-plus/icons-vue'
import type { ExamHistoryEntry } from '@/types'
import { buildTimelineItems, type ExamTimelineItem } from './examhistory'
import type { ScoreLevel } from './stability'
import ExamReportCard from './ExamReportCard.vue'
import ExamShareDialog from './ExamShareDialog.vue'

const PAGE = 10

const props = withDefaults(
  defineProps<{
    entries: ExamHistoryEntry[]
    loading?: boolean
    // 分享卡展示用节点名(宿主抽屉可透传节点显示名);缺省时分享卡回落打码占位。
    nodeName?: string
  }>(),
  { loading: false, nodeName: '' }
)

const emit = defineEmits<{ (e: 'exam'): void }>()

const items = computed<ExamTimelineItem[]>(() => buildTimelineItems(props.entries))
const visibleCount = ref(PAGE)
const visibleItems = computed(() => items.value.slice(0, visibleCount.value))
const hasMore = computed(() => items.value.length > visibleCount.value)

const selectedId = ref<number | null>(null)
const selectedReport = computed(
  () => props.entries.find((e) => e.id === selectedId.value)?.report ?? null
)

// 分享对话框状态:点击任一时间线条目的分享按钮即可唤起,无需先展开报告卡。
const shareVisible = ref(false)
const shareId = ref<number | null>(null)
const shareReport = computed(
  () => props.entries.find((e) => e.id === shareId.value)?.report ?? null
)
const shareExamTime = computed(() => {
  const entry = props.entries.find((e) => e.id === shareId.value)
  return entry?.created_at ?? ''
})

// 数据换节点/刷新时,重置展开与分页,避免残留上一个节点的选中态。
watch(
  () => props.entries,
  () => {
    selectedId.value = null
    visibleCount.value = PAGE
  }
)

const toggle = (id: number) => {
  selectedId.value = selectedId.value === id ? null : id
}
const showMore = () => {
  visibleCount.value += PAGE
}

const openShare = (id: number) => {
  shareId.value = id
  shareVisible.value = true
}

const tagType = (level: ScoreLevel): 'success' | 'warning' | 'danger' => {
  if (level === 'good') return 'success'
  if (level === 'fair') return 'warning'
  return 'danger'
}
</script>

<style scoped>
.exam-timeline-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: var(--ph-space-2);
}
.drawer-section-title {
  font-weight: 600;
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.exam-timeline-hint {
  padding: var(--ph-space-4);
  text-align: center;
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.exam-timeline-empty {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--ph-space-3);
  padding: var(--ph-space-4);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius-lg);
}
.exam-timeline-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.exam-timeline-item {
  border-bottom: 1px solid var(--ph-border-light);
}
.exam-timeline-row {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  width: 100%;
  padding: var(--ph-space-3) var(--ph-space-1);
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  font: inherit;
  color: inherit;
}
.exam-timeline-row:hover {
  background: var(--ph-bg-hover);
}
.exam-timeline-time {
  min-width: 72px;
  font-size: var(--ph-text-sm);
}
.exam-timeline-unlock {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.exam-timeline-share {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  padding: 0;
  margin-left: var(--ph-space-1);
  background: none;
  border: none;
  border-radius: var(--ph-radius-sm);
  cursor: pointer;
  color: var(--ph-text-secondary);
  transition: all 0.15s ease;
}
.exam-timeline-share:hover {
  background: var(--ph-bg-hover);
  color: var(--ph-color-primary);
}
.exam-timeline-caret {
  margin-left: auto;
  transition: transform 0.15s ease;
  color: var(--ph-text-secondary);
}
.exam-timeline-caret.is-open {
  transform: rotate(90deg);
}
.exam-timeline-report {
  margin: 0 0 var(--ph-space-3);
}
.exam-timeline-more {
  margin-top: var(--ph-space-2);
  text-align: center;
}
</style>
