<template>
  <div class="filter-zone">
    <el-alert v-if="detecting" type="info" :closable="false" class="detect-alert">
      <template #title>
        <div class="detect-alert-row">
          <span>正在检测节点解锁状态...</span>
          <el-button size="small" @click="emit('cancel-detect')">取消</el-button>
        </div>
      </template>
    </el-alert>

    <div class="filter-bar">
      <el-select v-model="region" placeholder="地区" clearable filterable class="ctl-region">
        <el-option
          v-for="r in regions"
          :key="r.Code"
          :label="`${r.Name} (${r.Code})`"
          :value="r.Code"
        />
      </el-select>
      <el-select v-model="available" placeholder="可用状态" clearable class="ctl-sm">
        <el-option label="可用" value="true" />
        <el-option label="不可用" value="false" />
      </el-select>
      <el-input
        v-model="source"
        placeholder="搜索机场"
        clearable
        class="ctl-source"
        @keyup.enter="emit('search')"
        @clear="emit('search')"
      />
      <el-button link type="primary" @click="more = !more">
        更多筛选
        <el-icon><ArrowUp v-if="more" /><ArrowDown v-else /></el-icon>
      </el-button>
    </div>

    <div v-show="more" class="filter-bar">
      <el-select v-model="type" placeholder="类型" clearable class="ctl-sm">
        <el-option v-for="t in NODE_TYPES" :key="t" :label="t" :value="t" />
      </el-select>
      <el-select v-model="blocked" placeholder="屏蔽状态" clearable class="ctl-sm">
        <el-option label="已屏蔽" value="true" />
        <el-option label="未屏蔽" value="false" />
      </el-select>
      <el-select v-model="stale" placeholder="上下架" clearable class="ctl-sm">
        <el-option label="已下架" value="true" />
        <el-option label="在架" value="false" />
      </el-select>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue'
import { NODE_TYPES } from '../utils'
import type { RegionItem } from '../composables/useNodeList'

// 常显最高频三项(地区/可用状态/搜索机场),低频项收进"更多筛选";
// 下拉 change 即生效由 useNodeFilters 的 watch 承担,这里不逐个发事件。
const region = defineModel<string>('region', { required: true })
const type = defineModel<string>('type', { required: true })
const available = defineModel<string>('available', { required: true })
const blocked = defineModel<string>('blocked', { required: true })
const stale = defineModel<string>('stale', { required: true })
const source = defineModel<string>('source', { required: true })

defineProps<{
  regions: RegionItem[]
  detecting: boolean
}>()

const emit = defineEmits<{
  (e: 'search'): void
  (e: 'cancel-detect'): void
}>()

const more = ref(false)
</script>

<style scoped>
.filter-zone {
  margin-bottom: var(--ph-space-3);
}
.detect-alert {
  margin-bottom: var(--ph-space-3);
}
.detect-alert-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.filter-bar {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  flex-wrap: wrap;
}
.filter-bar + .filter-bar {
  margin-top: var(--ph-space-2);
}
.ctl-region {
  width: 140px;
}
.ctl-sm {
  width: 120px;
}
.ctl-source {
  width: 160px;
}
</style>
