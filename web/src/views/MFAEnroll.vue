<template>
  <AuthShell :subtitle="step === 'codes' ? '保存恢复码' : '绑定两步验证'">
    <!-- ① 扫码 / 手动输入密钥 + ② 输入 6 位验证码确认 -->
    <template v-if="step === 'scan'">
      <el-alert
        type="warning"
        :closable="false"
        class="mfa-enroll__alert"
        title="账号需绑定验证器后才能使用"
      />

      <div v-loading="starting" class="mfa-enroll__qr">
        <img
          v-if="qrDataUrl"
          class="mfa-enroll__qr-img"
          :src="qrDataUrl"
          alt="两步验证绑定二维码"
          width="200"
          height="200"
        />
        <el-empty v-else-if="!starting" description="二维码生成失败，请用下方密钥手动添加" />
      </div>

      <p class="mfa-enroll__hint">
        用 Google Authenticator、1Password、Authy 等任一认证器扫码，或手动输入密钥：
      </p>

      <div class="mfa-enroll__secret">
        <code class="mfa-enroll__secret-text">{{ displaySecret || '获取中…' }}</code>
        <el-button
          :disabled="!secret"
          :icon="DocumentCopy"
          text
          type="primary"
          class="mfa-enroll__secret-copy"
          @click="copySecret"
        >
          复制
        </el-button>
      </div>

      <el-form class="mfa-enroll__form" label-position="top" @submit.prevent="handleConfirm">
        <el-form-item label="认证器上的 6 位验证码">
          <!-- 故意不用 v-model:归一化必须是唯一的写入口,
               否则 v-model 的原始值与 onCodeInput 的归一化值会互相打脸 -->
          <el-input
            :model-value="totpCode"
            placeholder="6 位数字"
            size="large"
            inputmode="numeric"
            autocomplete="one-time-code"
            :maxlength="TOTP_CODE_LENGTH"
            @update:model-value="onCodeInput"
            @keyup.enter="handleConfirm"
          />
        </el-form-item>
        <el-button
          type="primary"
          native-type="submit"
          :loading="confirming"
          :disabled="!codeComplete || starting"
          class="mfa-enroll__submit"
          size="large"
        >
          {{ confirming ? '验证中…' : '确认绑定' }}
        </el-button>
      </el-form>

      <p class="mfa-enroll__footer">绑定后每次登录都需要输入认证器上的动态验证码</p>
    </template>

    <!-- ③ 恢复码:仅此一次可见,必须勾选确认后才放行 -->
    <template v-else>
      <el-alert type="error" :closable="false" class="mfa-enroll__alert" title="恢复码只显示这一次">
        请立刻抄写或存入密码管理器。丢失认证器时，恢复码是你唯一的自助入口；它们离开本页后无法再次查看。
      </el-alert>

      <ul class="mfa-enroll__codes">
        <li v-for="code in recoveryCodes" :key="code" class="mfa-enroll__code">{{ code }}</li>
      </ul>

      <el-button
        :icon="DocumentCopy"
        class="mfa-enroll__copy-all"
        size="large"
        @click="copyRecoveryCodes"
      >
        复制全部恢复码
      </el-button>

      <el-checkbox v-model="savedAcknowledged" class="mfa-enroll__ack">
        我已保存恢复码
      </el-checkbox>

      <el-button
        type="primary"
        :disabled="!savedAcknowledged"
        :loading="finishing"
        class="mfa-enroll__submit"
        size="large"
        @click="handleFinish"
      >
        {{ finishing ? '处理中…' : '完成' }}
      </el-button>
    </template>
  </AuthShell>
</template>

<script setup lang="ts">
// 强制 MFA 绑定页(ticket 08)。两段式流程对应后端 POST /api/me/mfa/enroll:
// 无 totp_code 领密钥,带 totp_code 确认并换回一次性恢复码。
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ElMessage } from 'element-plus'
import { DocumentCopy } from '@element-plus/icons-vue'
import AuthShell from '@/components/AuthShell.vue'
import { confirmMFAEnroll, startMFAEnroll } from '@/api/mfa'
import { generateQRCode } from '@/composables/useQRCode'
import {
  TOTP_CODE_LENGTH,
  enrollConfirmErrorMessage,
  enrollStartErrorMessage,
  formatSecretForDisplay,
  isAlreadyEnrolled,
  isCompleteTOTPCode,
  normalizeTOTPCode
} from './mfa-enroll-utils'
import { copyText } from '@/utils/clipboard'

const router = useRouter()
const authStore = useAuthStore()

// step 'scan' = 扫码并确认;'codes' = 展示一次性恢复码。
// 两步之间不可回退:确认成功后 MFA 已在服务端启用,回到扫码页毫无意义。
const step = ref<'scan' | 'codes'>('scan')

const starting = ref(false)
const confirming = ref(false)
const finishing = ref(false)

const secret = ref('')
const qrDataUrl = ref('')
const totpCode = ref('')
const recoveryCodes = ref<string[]>([])
const savedAcknowledged = ref(false)

const displaySecret = computed(() => formatSecretForDisplay(secret.value))
const codeComplete = computed(() => isCompleteTOTPCode(totpCode.value))

// onCodeInput 归一化输入:认证器把码显示成 "123 456",粘贴进来不该因为
// 一个空格而验签失败。
const onCodeInput = (raw: string) => {
  totpCode.value = normalizeTOTPCode(raw)
}

// releaseUser 清掉强制位并回首页。绑定成功后会话依旧有效,清位才是真正放行的动作。
const releaseUser = async () => {
  authStore.clearMustEnrollMFA()
  // 从 /me 重取权威状态:清位只是本地乐观更新,服务端才是事实源。
  // 拉取失败不阻塞放行(守卫已按本地状态放行),下次刷新会自然纠正。
  await authStore.restore().catch(() => false)
  router.push('/')
}

// requestSecret 走第一段:领一个待确认的密钥并渲染二维码。
// 重复调用是安全的(后端只是重新 stage),所以刷新页面即可重来。
const requestSecret = async () => {
  starting.value = true
  try {
    const data = await startMFAEnroll()
    secret.value = data.secret
    // 二维码失败不该拖垮整页:密钥文本仍然可以手动录入,
    // 所以这里只清空图片并提示,不抛。
    try {
      qrDataUrl.value = await generateQRCode(data.otpauth_url)
    } catch {
      qrDataUrl.value = ''
      ElMessage.warning('二维码生成失败，请用密钥手动添加')
    }
  } catch (err) {
    // 409 = 早已绑定,本地强制位是旧的:直接放行,别把用户锁死在这页。
    if (isAlreadyEnrolled(err)) {
      ElMessage.info(enrollStartErrorMessage(err))
      await releaseUser()
      return
    }
    ElMessage.error(enrollStartErrorMessage(err))
  } finally {
    starting.value = false
  }
}

const handleConfirm = async () => {
  if (!codeComplete.value) {
    ElMessage.warning(`请输入认证器上的 ${TOTP_CODE_LENGTH} 位验证码`)
    return
  }
  confirming.value = true
  try {
    const data = await confirmMFAEnroll(totpCode.value)
    recoveryCodes.value = data.recovery_codes ?? []
    step.value = 'codes'
  } catch (err) {
    if (isAlreadyEnrolled(err)) {
      ElMessage.info(enrollConfirmErrorMessage(err))
      await releaseUser()
      return
    }
    ElMessage.error(enrollConfirmErrorMessage(err))
    // 码是一次性的:验证失败就清空,免得用户对着过期码反复点。
    totpCode.value = ''
  } finally {
    confirming.value = false
  }
}

const copySecret = async () => {
  try {
    await copyText(secret.value)
    ElMessage.success('密钥已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

const copyRecoveryCodes = async () => {
  try {
    await copyText(recoveryCodes.value.join('\n'))
    ElMessage.success('恢复码已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

const handleFinish = async () => {
  // 双保险:按钮已 disabled,这里再挡一次,避免程序化触发绕过确认。
  if (!savedAcknowledged.value) return
  finishing.value = true
  try {
    await releaseUser()
  } finally {
    finishing.value = false
  }
}

onMounted(requestSecret)
</script>

<style scoped>
.mfa-enroll__alert {
  margin-bottom: var(--ph-space-5);
}

/* ---------- 二维码 ---------- */
.mfa-enroll__qr {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 220px;
  margin-bottom: var(--ph-space-4);
}
.mfa-enroll__qr-img {
  width: 200px;
  height: 200px;
  border-radius: var(--ph-radius);
  /* 二维码必须在深色模式下也保持白底,否则认证器扫不出来 */
  background: #fff;
  padding: 8px;
  box-shadow: var(--ph-shadow-sm);
}

.mfa-enroll__hint {
  margin: 0 0 var(--ph-space-3);
  font-size: var(--ph-text-sm);
  line-height: 1.6;
  color: var(--ph-text-secondary);
}
/* ---------- 密钥 ---------- */
.mfa-enroll__secret {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  padding: 10px 12px;
  margin-bottom: var(--ph-space-5);
  border: 1px dashed var(--ph-border);
  border-radius: var(--ph-radius);
  background: var(--ph-bg-page);
}
.mfa-enroll__secret-text {
  flex: 1;
  min-width: 0;
  font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 13px;
  letter-spacing: 1px;
  word-break: break-all;
  color: var(--ph-text-primary);
}
.mfa-enroll__secret-copy {
  flex-shrink: 0;
}

/* ---------- 表单 ---------- */
.mfa-enroll__form :deep(.el-form-item) {
  margin-bottom: 20px;
}
.mfa-enroll__form :deep(.el-input__wrapper) {
  border-radius: var(--ph-radius);
  padding: 4px 12px;
}
.mfa-enroll__form :deep(.el-input__inner) {
  letter-spacing: 6px;
  text-align: center;
  font-size: 18px;
}
.mfa-enroll__submit {
  width: 100%;
  margin-top: var(--ph-space-1);
  font-weight: 600;
  letter-spacing: 2px;
  border-radius: var(--ph-radius);
}

/* ---------- 恢复码 ---------- */
.mfa-enroll__codes {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--ph-space-2);
  margin: 0 0 var(--ph-space-4);
  padding: var(--ph-space-3);
  list-style: none;
  border: 1px solid var(--ph-border);
  border-radius: var(--ph-radius);
  background: var(--ph-bg-page);
}
.mfa-enroll__code {
  font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 13px;
  letter-spacing: 1px;
  text-align: center;
  color: var(--ph-text-primary);
  user-select: all;
}

.mfa-enroll__copy-all {
  width: 100%;
  border-radius: var(--ph-radius);
}

.mfa-enroll__ack {
  display: flex;
  margin: var(--ph-space-4) 0 var(--ph-space-3);
}

.mfa-enroll__footer {
  margin: var(--ph-space-5) 0 0;
  text-align: center;
  font-size: 12px;
  color: var(--ph-text-secondary);
}

@media (max-width: 480px) {
  .mfa-enroll__codes {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
