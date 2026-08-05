<template>
  <!-- 用量信息手填字段(手动机场创建/编辑/粘贴导入共用):
       全部可选,留空即不展示;引导文案告诉用户在机场面板哪里找到这三项。
       webPageOnly 模式(拉取型机场):只渲染官网——用量由订阅响应头自动捕获,无需手填。 -->
  <template v-if="!webPageOnly">
    <el-form-item label="剩余流量">
      <el-input-number
        v-model="model.remainingGb"
        :min="0"
        :precision="2"
        :controls="false"
        placeholder="如 850"
        class="usage-input"
      />
      <span class="unit">GB</span>
    </el-form-item>
    <el-form-item label="总流量">
      <el-input-number
        v-model="model.totalGb"
        :min="0"
        :precision="2"
        :controls="false"
        placeholder="如 1000"
        class="usage-input"
      />
      <span class="unit">GB</span>
    </el-form-item>
    <el-form-item label="过期日期">
      <el-date-picker
        v-model="model.expireDate"
        type="date"
        value-format="YYYY-MM-DD"
        placeholder="套餐过期日"
        class="usage-input"
      />
    </el-form-item>
  </template>
  <el-form-item label="官网">
    <el-input v-model="model.webPageUrl" placeholder="https://...(机场面板首页地址)" />
  </el-form-item>
  <el-form-item>
    <div class="usage-guide">
      <template v-if="webPageOnly">
        可选,留空即不展示。拉取型机场的官网通常由订阅响应头自动捕获;
        手填后,仅当响应头不提供官网时保留手填值(响应头提供时以其为准)。
      </template>
      <template v-else>
        以上全部可选,留空即不展示。可在机场面板的「套餐/订阅」页找到:剩余与总流量 (面板常以 GB
        展示)、套餐过期时间;官网即面板首页地址。
      </template>
    </div>
  </el-form-item>
</template>

<script setup lang="ts">
import type { UsageFormValue } from '@/views/airport-utils'

// 双向绑定的用量表单值(父组件负责与接口载荷互转,见 airport-utils)。
const model = defineModel<UsageFormValue>({ required: true })

// webPageOnly: 只渲染官网字段(拉取型机场手填入口;用量三项不渲染)
withDefaults(defineProps<{ webPageOnly?: boolean }>(), { webPageOnly: false })
</script>

<style scoped>
.usage-input {
  width: 180px;
}
.unit {
  margin-left: var(--ph-space-2);
  color: var(--ph-text-secondary);
}
.usage-guide {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
  line-height: 1.6;
}
</style>
