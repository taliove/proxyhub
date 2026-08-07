<template>
  <!-- 精选数量标签(issue #80):「精选 N 个节点」/「全量」;点击上抛 open 由父级打开选择器 -->
  <el-tag :type="count ? 'warning' : 'info'" size="small" class="picks-tag" @click="emit('open')">
    {{ label }}
  </el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Endpoint } from '@/types'
import { nodePicksCount, nodePicksLabel } from './endpoint-nodepicks-utils'

const props = defineProps<{ endpoint: Endpoint }>()
const emit = defineEmits<{ (e: 'open'): void }>()

const count = computed(() => nodePicksCount(props.endpoint))
const label = computed(() => nodePicksLabel(props.endpoint))
</script>

<style scoped>
.picks-tag {
  cursor: pointer;
}
</style>
