<template>
  <el-card class="top-nodes" shadow="never">
    <template #header>
      <div class="panel-header">
        <span class="panel-title">优质节点</span>
        <span class="panel-caption">按体检总分取 Top 10</span>
      </div>
    </template>
    <div v-if="loading" class="panel-empty">加载中...</div>
    <div v-else-if="failed" class="panel-empty">加载失败,请稍后重试</div>
    <div v-else-if="items.length === 0" class="panel-empty">
      还没有体检过的节点,
      <router-link class="empty-link" to="/nodes">去节点页跑体检</router-link>
    </div>
    <ol v-else class="node-list">
      <li
        v-for="(item, index) in items"
        :key="item.node_key"
        class="node-item"
        :class="{ 'is-unavailable': !item.available }"
      >
        <span class="item-rank">{{ index + 1 }}</span>
        <span class="item-region">{{ regionDisplay(item.region) }}</span>
        <span class="item-score" :class="`grade-${item.score.grade}`">
          <span class="score-value">{{ displayScore(item.score.total) }}</span>
          <span class="score-grade">{{ gradeLabel(item.score.grade) }}</span>
        </span>
        <span class="item-tags">
          <el-tag
            v-for="tag in visibleTags(item.tags)"
            :key="tag"
            size="small"
            effect="plain"
            class="tag-chip"
          >
            {{ tagLabel(tag) }}
          </el-tag>
          <span v-if="overflowTagCount(item.tags) > 0" class="tags-overflow">
            +{{ overflowTagCount(item.tags) }}
          </span>
        </span>
        <span class="item-source" :title="item.source">{{ item.source }}</span>
        <el-tag v-if="!item.available" size="small" type="info" effect="plain" class="item-state">
          不可用
        </el-tag>
        <span v-if="canShare(item)" class="item-ops">
          <el-button link type="primary" size="small" @click="copyLink(item)">复制链接</el-button>
          <el-button link type="primary" size="small" @click="showQR(item)">二维码</el-button>
        </span>
      </li>
    </ol>
    <QRCodeDialog
      ref="qrDialog"
      v-model="qrVisible"
      title="节点分享二维码"
      hint="使用客户端扫码即可导入节点"
    />
  </el-card>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import type { Node } from '@/types'
import QRCodeDialog from '@/components/QRCodeDialog.vue'
import { gradeLabel } from '@/components/exam/score'
import { canGenerateShareLink, copyNodeLink, getNodeShareLink } from '@/composables/useNodeShare'
import { tagLabel } from '@/utils/taglabels'
import { regionDisplay } from '@/views/nodes/nodecells'
import { useTopNodes, prioritizeTags, type TopNodeItem } from '../composables/useTopNodes'

const { items, loading, failed } = useTopNodes()

// 标签 chips 最多直出 3 个,其余收敛为 "+N"(重点标签已由 prioritizeTags 提到前面)。
const MAX_VISIBLE_TAGS = 3
const visibleTags = (tags: string[]): string[] => prioritizeTags(tags).slice(0, MAX_VISIBLE_TAGS)
const overflowTagCount = (tags: string[]): number => Math.max(0, tags.length - MAX_VISIBLE_TAGS)

const displayScore = (total: number): number => Math.round(total)

// 分享函数只消费 node_key 与 type 两个字段,按 Node 结构最小构造。
const asShareNode = (item: TopNodeItem): Node =>
  ({ node_key: item.node_key, type: item.type }) as Node

const canShare = (item: TopNodeItem): boolean => canGenerateShareLink(asShareNode(item))

const copyLink = (item: TopNodeItem): void => {
  void copyNodeLink(asShareNode(item))
}

const qrVisible = ref(false)
const qrDialog = ref<InstanceType<typeof QRCodeDialog>>()

const showQR = async (item: TopNodeItem): Promise<void> => {
  try {
    const uri = await getNodeShareLink(asShareNode(item))
    qrDialog.value?.show(uri)
  } catch (error) {
    ElMessage.error(`获取分享链接失败：${error instanceof Error ? error.message : String(error)}`)
  }
}
</script>

<style scoped>
.top-nodes {
  border-radius: var(--ph-radius-lg);
}
.panel-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--ph-space-2);
}
.panel-title {
  font-size: var(--ph-text-md);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.panel-caption {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
}
.panel-empty {
  padding: var(--ph-space-6) 0;
  text-align: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-placeholder);
}
.empty-link {
  color: var(--ph-color-primary);
  text-decoration: none;
}
.node-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.node-item {
  display: flex;
  align-items: center;
  gap: var(--ph-space-3);
  padding: var(--ph-space-2) 0;
}
.node-item + .node-item {
  border-top: 1px solid var(--ph-border-light);
}
.item-rank {
  flex-shrink: 0;
  width: 20px;
  text-align: right;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
}
.item-region {
  flex-shrink: 0;
  min-width: 64px;
  font-size: var(--ph-text-sm);
  font-weight: 600;
  color: var(--ph-text-primary);
}
.item-score {
  flex-shrink: 0;
  display: inline-flex;
  align-items: baseline;
  gap: var(--ph-space-1);
}
.score-value {
  font-size: var(--ph-text-sm);
  font-weight: 600;
}
.score-grade {
  font-size: var(--ph-text-xs);
}
/* 档位语义色:与 ExamReportLayout 的档色定义一致(gradeColorVar) */
.node-item:not(.is-unavailable) .grade-excellent,
.node-item:not(.is-unavailable) .grade-good {
  color: var(--ph-success);
}
.node-item:not(.is-unavailable) .grade-fair {
  color: var(--ph-warning);
}
.node-item:not(.is-unavailable) .grade-poor,
.node-item:not(.is-unavailable) .grade-very_poor {
  color: var(--ph-danger);
}
/* 不可用节点视觉降级:主信息降为占位灰 */
.node-item.is-unavailable .item-region,
.node-item.is-unavailable .item-score {
  font-weight: 400;
  color: var(--ph-text-placeholder);
}
.item-tags {
  flex: 1;
  min-width: 0;
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
  overflow: hidden;
}
.tag-chip {
  flex-shrink: 0;
}
.tags-overflow {
  flex-shrink: 0;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-placeholder);
}
.item-source {
  flex-shrink: 0;
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.item-state {
  flex-shrink: 0;
}
.item-ops {
  flex-shrink: 0;
  display: inline-flex;
  align-items: center;
}
</style>
