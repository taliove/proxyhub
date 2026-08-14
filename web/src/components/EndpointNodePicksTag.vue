<template>
  <!-- 精选数量标签(issue #80):「精选 N 个节点」/「全量」;点击上抛 open 由父级打开选择器。
       精选配置损坏(issue #91):后端 picks_error 标记,红色警示 + 悬停说明 -->
  <el-tooltip
    :disabled="!broken"
    content="精选配置已损坏，当前按全量下发；请重新选择精选节点。"
    placement="top"
  >
    <el-tag :type="tagType" size="small" class="picks-tag" @click="emit('open')">
      {{ label }}
    </el-tag>
  </el-tooltip>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Endpoint } from '@/types'
import { nodePicksBroken, nodePicksCount, nodePicksLabel } from './endpoint-nodepicks-utils'

const props = defineProps<{ endpoint: Endpoint }>()
const emit = defineEmits<{ (e: 'open'): void }>()

const broken = computed(() => nodePicksBroken(props.endpoint))
const count = computed(() => nodePicksCount(props.endpoint))
const tagType = computed(() => (broken.value ? 'danger' : count.value ? 'warning' : 'info'))
const label = computed(() => (broken.value ? '精选配置异常' : nodePicksLabel(props.endpoint)))
</script>

<style scoped>
.picks-tag {
  cursor: pointer;
}
</style>
