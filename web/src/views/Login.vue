<template>
  <!-- 外壳(背景/主题/卡片/品牌头)复用 AuthShell,与改密页、MFA 绑定页同源 -->
  <AuthShell subtitle="代理订阅聚合系统">
    <!-- 第一步:凭据(+ 必要时验证码) -->
    <el-form
      v-if="!mfaActive"
      ref="formRef"
      :model="form"
      :rules="rules"
      class="login__form"
      @submit.prevent="handleLogin"
    >
      <el-form-item prop="username">
        <el-input
          v-model="form.username"
          placeholder="用户名"
          size="large"
          autocomplete="username"
          clearable
        >
          <template #prefix
            ><el-icon><User /></el-icon
          ></template>
        </el-input>
      </el-form-item>
      <el-form-item prop="password">
        <el-input
          v-model="form.password"
          type="password"
          placeholder="密码"
          size="large"
          autocomplete="current-password"
          show-password
          @keyup.enter="handleLogin"
        >
          <template #prefix
            ><el-icon><Lock /></el-icon
          ></template>
        </el-input>
      </el-form-item>
      <!-- 验证码块:后端 401 带 captcha_required 后才出现,首屏零请求 -->
      <el-form-item v-if="captchaVisible">
        <CaptchaField
          v-model="captchaAnswer"
          :image-src="captchaImage"
          :refreshing="captchaRefreshing"
          @refresh="refreshCaptcha"
          @submit="handleLogin"
        />
      </el-form-item>
      <el-button
        type="primary"
        native-type="submit"
        :loading="loading"
        class="login__submit"
        size="large"
      >
        {{ loading ? '登录中…' : '登 录' }}
      </el-button>
    </el-form>

    <!-- 第二步:两步验证。与第一步互斥渲染,MFA 态不需要图形验证码 -->
    <LoginMFAForm
      v-else
      :code="mfaCode"
      :mode="mfaMode"
      :trust-ip="mfaTrustIP"
      :submitting="mfaSubmitting"
      :code-complete="mfaCodeComplete"
      @update:code="mfa.setCode"
      @update:trust-ip="(v: boolean) => (mfaTrustIP = v)"
      @toggle-mode="mfa.toggleMode()"
      @submit="handleMFASubmit"
      @back="handleBackToPassword"
    />

    <p class="login__footer">ProxyHub · 安全高效的订阅聚合</p>
  </AuthShell>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { User, Lock } from '@element-plus/icons-vue'
import AuthShell from '@/components/AuthShell.vue'
import CaptchaField from '@/components/CaptchaField.vue'
import LoginMFAForm from '@/components/LoginMFAForm.vue'
import { login, type LoginResponse } from '@/api/auth'
import { useLoginCaptcha } from '@/composables/useLoginCaptcha'
import { useLoginMFA } from '@/composables/useLoginMFA'
import { captchaRequiredFromError, loginErrorMessage } from './login-utils'

const router = useRouter()
const authStore = useAuthStore()

const loading = ref(false)
const formRef = ref<FormInstance>()

// 验证码状态机(ticket 04):首屏休眠,后端 401 带 captcha_required 才激活。
const captcha = useLoginCaptcha()
const {
  answer: captchaAnswer,
  imageSrc: captchaImage,
  refreshing: captchaRefreshing,
  visible: captchaVisible,
  fetchChallenge: refreshCaptcha
} = captcha

// 两步登录第二态(ticket 09):后端 200 带 mfa_required 才激活,
// 与验证码块互斥渲染(MFA 态不需要图形验证码)。
const mfa = useLoginMFA()
const {
  active: mfaActive,
  code: mfaCode,
  mode: mfaMode,
  trustIP: mfaTrustIP,
  submitting: mfaSubmitting,
  codeComplete: mfaCodeComplete
} = mfa

const form = reactive({
  username: '',
  password: ''
})

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }]
}

// completeLogin 是两态共用的唯一放行分支:登录成功的语义与去向不该因为
// 走了几步验证而不同。顺序与守卫一致:改密优先,后端 requirePasswordChanged
// 包着绑定接口。
const completeLogin = (data: LoginResponse | null) => {
  const role = data?.user?.role ?? data?.role ?? ''
  const mustChange = data?.user?.must_change_password ?? false
  const mustEnroll = data?.user?.must_enroll_mfa ?? false
  authStore.setAuth(form.username, role, mustChange, mustEnroll)
  if (mustChange) {
    router.push('/change-password')
    return
  }
  // 未绑定 MFA(ticket 08):直接送去绑定页,不必先撞一次 403。
  if (mustEnroll) {
    router.push('/mfa/enroll')
    return
  }
  ElMessage.success('登录成功')
  router.push('/')
}

const handleLogin = async () => {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  // 验证码已出现时答案必填:空答案提交只会白白累加该 IP 的失败计数(可能自封)
  if (captchaVisible.value && captchaAnswer.value.trim() === '') {
    ElMessage.warning('请输入验证码')
    return
  }
  loading.value = true
  try {
    // Login response carries the user profile (ticket 02); role drives admin UI gating,
    // must_change_password (ticket 04) routes the user to the forced password change.
    // captcha.payload() 在休眠期返回空对象,正常登录路径的请求体与从前完全一致。
    const data = await login({ ...form, ...captcha.payload() })
    // 密码对了但该 IP 未受信(ticket 06):这是 200 而不是错误,必须看响应体分流。
    if (data?.mfa_required === true && data?.mfa_pending_token) {
      mfa.start(data.mfa_pending_token)
      return
    }
    completeLogin(data)
  } catch (err) {
    // 登录失败自成一体:请求已 skipAuthRedirect + skipErrorToast(见 api/auth.ts),
    // 由本页提示,并在后端仍要求验证码时换一张新图、清空旧答案。
    ElMessage.error(loginErrorMessage(err))
    await captcha.handleFailure(captchaRequiredFromError(err))
  } finally {
    loading.value = false
  }
}

const handleMFASubmit = async () => {
  const data = await mfa.submit()
  // null = 已失败并提示过(码错/过期/超次都是同一个 401,不区分);
  // 句柄若已被服务端销毁,composable 已把页面退回第一态。
  if (data === null) return
  completeLogin(data)
}

// handleBackToPassword 手动退回第一态。第一态的验证码状态机不受影响:
// 后端若仍要求验证码,那张图与答案仍在原处等着。
const handleBackToPassword = () => {
  mfa.reset()
  form.password = ''
}
</script>

<style scoped>
/* 外壳样式(背景/主题/卡片/品牌头)已归 AuthShell,此处只留登录表单自身 */
.login__form :deep(.el-form-item) {
  margin-bottom: 22px;
}
.login__form :deep(.el-input__wrapper) {
  border-radius: var(--ph-radius);
  padding: 4px 12px;
}
.login__submit {
  width: 100%;
  margin-top: var(--ph-space-1);
  font-weight: 600;
  letter-spacing: 2px;
  border-radius: var(--ph-radius);
}

.login__footer {
  margin: var(--ph-space-5) 0 0;
  text-align: center;
  font-size: 12px;
  color: var(--ph-text-secondary);
}
</style>
