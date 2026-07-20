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
      <el-select v-model="source" placeholder="来源" clearable filterable class="ctl-md">
        <el-option label="自建节点" :value="SELF_HOSTED" />
        <el-option v-for="s in sources" :key="s" :label="s" :value="s" />
      </el-select>
      <el-select v-model="region" placeholder="地区" clearable filterable class="ctl-region">
        <el-option
          v-for="r in regions"
          :key="r.Code"
          :label="`${r.Name} (${r.Code})`"
          :value="r.Code"
        />
      </el-select>
      <el-input v-model="keyword" placeholder="搜索名称/服务器" clearable class="ctl-source" />
      <el-button link type="primary" @click="more = !more">
        更多筛选
        <el-icon><ArrowUp v-if="more" /><ArrowDown v-else /></el-icon>
      </el-button>
    </div>

    <div v-show="more" class="filter-bar">
      <el-select v-model="type" placeholder="类型" clearable class="ctl-sm">
        <el-option v-for="t in NODE_TYPES" :key="t" :label="t" :value="t" />
      </el-select>
      <el-select v-model="availableStr" placeholder="可用状态" clearable class="ctl-sm">
        <el-option label="可用" value="true" />
        <el-option label="不可用" value="false" />
      </el-select>
      <el-select v-model="blockedStr" placeholder="屏蔽状态" clearable class="ctl-sm">
        <el-option label="已屏蔽" value="true" />
        <el-option label="未屏蔽" value="false" />
      </el-select>
      <el-select v-model="staleStr" placeholder="上下架" clearable class="ctl-sm">
        <el-option label="已下架" value="true" />
        <el-option label="在架" value="false" />
      </el-select>
      <el-select
        v-model="tags"
        placeholder="标签"
        multiple
        collapse-tags
        clearable
        class="ctl-md"
        :disabled="tagOptions.length === 0"
      >
        <el-option v-for="t in tagOptions" :key="t" :label="t" :value="t" />
      </el-select>
      <el-select
        v-model="unlock"
        placeholder="解锁能力"
        multiple
        collapse-tags
        clearable
        class="ctl-md"
        :disabled="unlockTargets.length === 0"
      >
        <el-option v-for="u in unlockTargets" :key="u" :label="u" :value="u" />
      </el-select>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ArrowDown, ArrowUp } from '@element-plus/icons-vue'
import { NODE_TYPES, SELF_HOSTED } from '../utils'
import type { RegionItem } from '../composables/useNodePool'

// 结构化筛选条件的双向绑定(父层持有 criteria,child 只按字段收发,不改 props)。
// 客户端过滤即时生效(谓词见 predicates.ts),故无需 search 事件。
const source = defineModel<string>('source', { required: true })
const region = defineModel<string>('region', { required: true })
const keyword = defineModel<string>('keyword', { required: true })
const type = defineModel<string>('type', { required: true })
const available = defineModel<boolean | null>('available', { required: true })
const blocked = defineModel<boolean | null>('blocked', { required: true })
const stale = defineModel<boolean | null>('stale', { required: true })
const tags = defineModel<string[]>('tags', { required: true })
const unlock = defineModel<string[]>('unlock', { required: true })

defineProps<{
  regions: RegionItem[]
  sources: string[]
  tagOptions: string[]
  unlockTargets: string[]
  detecting: boolean
}>()

const emit = defineEmits<{
  (e: 'cancel-detect'): void
}>()

// 三态布尔 <-> el-select 字符串:清空 -> null(不筛选)。
const triProxy = (model: { value: boolean | null }) =>
  computed<string>({
    get: () => (model.value === null ? '' : String(model.value)),
    set: (v) => {
      model.value = v === '' ? null : v === 'true'
    }
  })
const availableStr = triProxy(available)
const blockedStr = triProxy(blocked)
const staleStr = triProxy(stale)

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
  width: 150px;
}
.ctl-sm {
  width: 120px;
}
.ctl-md {
  width: 170px;
}
.ctl-source {
  width: 180px;
}
</style>
