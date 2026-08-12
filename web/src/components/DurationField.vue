<template>
  <div class="duration-field">
    <el-input-number v-model="amount" :min="1" :max="maxAmount" :precision="0" />
    <el-select v-model="unit" class="duration-field__unit">
      <el-option label="分钟" value="m" />
      <el-option label="小时" value="h" />
      <el-option label="天" value="d" />
    </el-select>
  </div>
</template>

<script setup lang="ts">
// DurationField 把 Go duration 字符串(如 1h/24h/90m)约束为「数字 + 单位」结构化输入,
// 替代自由文本防手抖(Settings 封禁时长/黑名单时长, critique P1)。
// 序列化:分钟→90m,小时→24h,天→168h(7*24h);反解析仅认 \d+(m|h),其余落默认 1 小时。
import { ref, watch } from 'vue'

const props = defineProps<{
  modelValue: string | number
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: string): void
}>()

const parse = (raw: string | number): { amount: number; unit: 'm' | 'h' | 'd' } => {
  const m = /^(\d+)(m|h)$/.exec(String(raw).trim())
  if (!m) return { amount: 1, unit: 'h' }
  const n = Number(m[1])
  if (m[2] === 'm') return { amount: Math.max(1, n), unit: 'm' }
  if (n >= 24 && n % 24 === 0) return { amount: n / 24, unit: 'd' }
  return { amount: Math.max(1, n), unit: 'h' }
}

const initial = parse(props.modelValue)
const amount = ref(initial.amount)
const unit = ref<'m' | 'h' | 'd'>(initial.unit)

// 外部重载(如租户键「重置」后重新拉取)时同步回显
watch(
  () => props.modelValue,
  (v) => {
    const p = parse(v)
    amount.value = p.amount
    unit.value = p.unit
  }
)

const serialize = () => {
  const n = amount.value ?? 1
  if (unit.value === 'm') return `${n}m`
  if (unit.value === 'd') return `${n * 24}h`
  return `${n}h`
}

watch([amount, unit], () => emit('update:modelValue', serialize()))

const MAX_BY_UNIT = { m: 43200, h: 720, d: 30 } as const // 上限 30 天
const maxAmount = ref(MAX_BY_UNIT[unit.value])
watch(unit, (u) => {
  maxAmount.value = MAX_BY_UNIT[u]
})
</script>

<style scoped>
.duration-field {
  display: flex;
  gap: var(--ph-space-2);
}
.duration-field__unit {
  width: 96px;
  flex-shrink: 0;
}
</style>
