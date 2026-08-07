<template>
  <!-- 登录第二步:两步验证(ticket 09)。仅在 POST /api/login 回
       {ok:false, mfa_required:true} 后被渲染,与第一步的凭据/图形验证码互斥。
       受控组件:码与信任勾选都由登录页(useLoginMFA)持有,本组件不存状态,
       归一化因此只有一个写入口。 -->
  <el-form class="login-mfa" @submit.prevent="emit('submit')">
    <p class="login-mfa__hint">
      {{
        mode === 'totp'
          ? '密码已通过。请输入认证器上的 6 位动态验证码。'
          : '密码已通过。请输入一个未使用过的恢复码（XXXX-XXXX-XXXX）。'
      }}
    </p>

    <el-form-item>
      <!-- 故意不用 v-model:归一化必须是唯一写入口,
           否则 v-model 的原始值与归一化后的值会互相打脸。
           :key 让切换码型时输入框重建,清掉浏览器的输入法/自动填充残留 -->
      <el-input
        :key="mode"
        class="login-mfa__code"
        :model-value="code"
        :placeholder="mode === 'totp' ? '6 位数字验证码' : 'XXXX-XXXX-XXXX'"
        size="large"
        :inputmode="mode === 'totp' ? 'numeric' : 'text'"
        :autocomplete="mode === 'totp' ? 'one-time-code' : 'off'"
        :maxlength="mode === 'totp' ? TOTP_CODE_LENGTH : RECOVERY_CODE_LENGTH"
        @update:model-value="(v: string) => emit('update:code', v)"
        @keyup.enter="emit('submit')"
      >
        <template #prefix
          ><el-icon><Key /></el-icon
        ></template>
      </el-input>
    </el-form-item>

    <el-checkbox
      :model-value="trustIp"
      class="login-mfa__trust"
      @update:model-value="(v: boolean | string | number) => emit('update:trustIp', v === true)"
    >
      信任此 IP 30 天（此后从该网络登录不再要求验证码）
    </el-checkbox>

    <el-button
      type="primary"
      native-type="submit"
      :loading="submitting"
      :disabled="!codeComplete"
      class="login-mfa__submit"
      size="large"
    >
      {{ submitting ? '验证中…' : '验 证' }}
    </el-button>

    <div class="login-mfa__actions">
      <el-button text type="primary" @click="emit('toggle-mode')">
        {{ mode === 'totp' ? '用恢复码登录' : '用认证器验证码登录' }}
      </el-button>
      <!-- 401 在协议上不区分"码错"与"句柄没了",所以退回第一态必须是
           一个常在的显式入口,而不是靠猜错误细节自动触发 -->
      <el-button text @click="emit('back')">返回重新登录</el-button>
    </div>
  </el-form>
</template>

<script setup lang="ts">
import { Key } from '@element-plus/icons-vue'
import { RECOVERY_CODE_LENGTH, TOTP_CODE_LENGTH, type LoginMFAMode } from '@/views/login-mfa-utils'

defineProps<{
  // code 是已归一化的当前输入(受控,由登录页持有)
  code: string
  // mode 决定输入框的形态与提示,请求体两种码共用一个 code 字段
  mode: LoginMFAMode
  trustIp: boolean
  submitting: boolean
  // codeComplete 为假时禁用提交:短码只会白白烧掉 5 次尝试预算之一
  codeComplete: boolean
}>()

const emit = defineEmits<{
  'update:code': [value: string]
  'update:trustIp': [value: boolean]
  'toggle-mode': []
  submit: []
  back: []
}>()
</script>

<style scoped>
.login-mfa :deep(.el-form-item) {
  margin-bottom: 22px;
}
.login-mfa :deep(.el-input__wrapper) {
  border-radius: var(--ph-radius);
  padding: 4px 12px;
}

.login-mfa__hint {
  margin: 0 0 var(--ph-space-4);
  font-size: var(--ph-text-sm);
  line-height: 1.6;
  color: var(--ph-text-secondary);
}
.login-mfa__code :deep(input) {
  letter-spacing: 3px;
}

.login-mfa__trust {
  display: flex;
  margin-bottom: var(--ph-space-4);
  height: auto;
  white-space: normal;
}
.login-mfa__trust :deep(.el-checkbox__label) {
  font-size: 12px;
  line-height: 1.5;
  color: var(--ph-text-secondary);
  white-space: normal;
}

.login-mfa__submit {
  width: 100%;
  font-weight: 600;
  letter-spacing: 2px;
  border-radius: var(--ph-radius);
}

.login-mfa__actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--ph-space-2);
  margin-top: var(--ph-space-3);
}
</style>
