<template>
  <span class="tenant-badge">
    <el-tag size="small" effect="plain" :type="custom ? 'warning' : 'info'">
      {{ custom ? '已自定义' : '跟随全局默认' }}
    </el-tag>
    <el-button v-if="custom" size="small" link type="danger" @click="ctx?.reset(k)">
      重置
    </el-button>
  </span>
</template>

<script setup lang="ts">
// TenantBadge 租户级键的状态徽标:跟随全局默认 / 已自定义 + 重置。
// 从 Settings.vue 抽离(400 行门禁);状态经 provide/inject 注入,调用点只传 k。
import { computed, inject, type Ref } from 'vue'

export interface TenantBadgeCtx {
  overridden: Ref<Record<string, boolean>>
  reset: (k: string) => void
}

const props = defineProps<{ k: string }>()

const ctx = inject<TenantBadgeCtx>('tenantBadge')
const custom = computed(() => ctx?.overridden.value[props.k] === true)
</script>

<style scoped>
.tenant-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
  margin-left: var(--ph-space-2);
  vertical-align: middle;
}
</style>
