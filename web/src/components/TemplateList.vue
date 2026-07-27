<template>
  <div class="template-list">
    <div class="list-header">模板库</div>
    <div class="list-body">
      <div
        v-for="tmpl in templates"
        :key="tmpl.id"
        :class="['list-item', { active: selectedId === tmpl.id }]"
        @click="$emit('select', tmpl)"
      >
        <div class="item-name">
          {{ tmpl.name }}
          <el-tag v-if="tmpl.is_default" type="success" size="small">默认</el-tag>
        </div>
        <div class="item-meta">{{ tmpl.ref_count }} 个订阅地址使用</div>
      </div>
      <div v-if="templates.length === 0" class="list-empty">暂无模板，点击右上角新建</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { Template } from '@/api/templates'

defineProps<{
  templates: Template[]
  selectedId: number | null
}>()

defineEmits<{
  select: [template: Template]
}>()
</script>

<style scoped>
.template-list {
  width: 280px;
  flex-shrink: 0;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius-sm);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.list-header {
  padding: var(--ph-space-3);
  font-weight: 600;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-primary);
  border-bottom: 1px solid var(--ph-border);
  background: var(--ph-bg-hover);
}

.list-body {
  flex: 1;
  overflow-y: auto;
}

.list-item {
  padding: var(--ph-space-3);
  border-bottom: 1px solid var(--ph-border);
  cursor: pointer;
  transition: background-color 0.15s;
}

.list-item:hover {
  background: var(--ph-bg-hover);
}

.list-item.active {
  background: var(--el-color-primary-light-9);
  border-left: 3px solid var(--el-color-primary);
}

.item-name {
  font-size: var(--ph-text-sm);
  font-weight: 500;
  color: var(--ph-text-primary);
  margin-bottom: var(--ph-space-1);
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}

.item-meta {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}

.list-empty {
  padding: var(--ph-space-4);
  text-align: center;
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
</style>
