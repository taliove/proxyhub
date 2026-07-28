<template>
  <!-- 登录验证码块(ticket 04):仅在后端 401 带 captcha_required 后才被渲染。
       图片来自 GET /api/captcha 的 image_base64(已是 data URI,直接作 src)。 -->
  <div class="captcha">
    <el-input
      :model-value="modelValue"
      placeholder="验证码"
      size="large"
      maxlength="8"
      autocomplete="off"
      class="captcha__input"
      @update:model-value="(v: string) => emit('update:modelValue', v)"
      @keyup.enter="emit('submit')"
    >
      <template #prefix
        ><el-icon><Key /></el-icon
      ></template>
    </el-input>

    <button
      class="captcha__image"
      type="button"
      :disabled="refreshing"
      title="点击换一张"
      aria-label="点击换一张验证码"
      @click="emit('refresh')"
    >
      <img v-if="imageSrc" :src="imageSrc" alt="验证码图片" class="captcha__img" />
      <span v-else class="captcha__placeholder">{{ refreshing ? '加载中…' : '加载失败' }}</span>
    </button>

    <el-button
      link
      type="primary"
      :loading="refreshing"
      class="captcha__reload"
      @click="emit('refresh')"
    >
      换一张
    </el-button>
  </div>
</template>

<script setup lang="ts">
import { Key } from '@element-plus/icons-vue'

defineProps<{
  // modelValue 是用户填写的答案(受控,由登录页持有)
  modelValue: string
  // imageSrc 为空表示签发失败,展示占位文案而不是破图
  imageSrc: string
  refreshing: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
  refresh: []
  submit: []
}>()
</script>

<style scoped>
.captcha {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.captcha__input {
  flex: 1;
  min-width: 0;
}
.captcha__input :deep(.el-input__wrapper) {
  border-radius: var(--ph-radius);
  padding: 4px 12px;
}

/* 图片本身可点击换一张:按钮语义保证键盘可达 */
.captcha__image {
  flex: none;
  width: 120px;
  height: 44px;
  padding: 0;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius);
  background: var(--ph-bg-surface);
  cursor: pointer;
  overflow: hidden;
  display: flex;
  align-items: center;
  justify-content: center;
}
.captcha__image:disabled {
  cursor: progress;
}
.captcha__img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}
.captcha__placeholder {
  font-size: 12px;
  color: var(--ph-text-secondary);
}
.captcha__reload {
  flex: none;
  font-size: 12px;
}
</style>
