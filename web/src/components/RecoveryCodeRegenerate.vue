<template>
  <div class="recovery-regen">
    <el-alert
      type="warning"
      :closable="false"
      class="settings-alert"
      title="重新生成会作废当前全部恢复码"
    >
      恢复码是丢失认证器时的唯一自助入口。重新生成后旧码立即失效，新码只显示这一次。
      恢复码疑似泄露、或已用掉大半时才需要这么做。
    </el-alert>

    <el-button type="warning" @click="openConfirm">重新生成恢复码</el-button>

    <!-- ① 二次确认:必须带一个当前 TOTP 或未用过的恢复码,后端强制校验 -->
    <el-dialog
      v-model="confirmVisible"
      title="重新生成恢复码"
      width="420px"
      :close-on-click-modal="false"
    >
      <p class="regen-hint">请输入认证器上的 6 位动态码，或任一未使用的恢复码，以确认本次操作：</p>
      <el-input
        v-model="confirmCode"
        placeholder="6 位动态码或恢复码"
        autocomplete="one-time-code"
        @keyup.enter="submitRegenerate"
      />
      <template #footer>
        <el-button @click="confirmVisible = false">取消</el-button>
        <el-button
          type="warning"
          :loading="submitting"
          :disabled="confirmCode.trim() === ''"
          @click="submitRegenerate"
        >
          {{ submitting ? '生成中…' : '确认生成' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- ② 新恢复码:仅此一次可见,必须勾选确认后才放行关闭 -->
    <el-dialog
      v-model="codesVisible"
      title="新恢复码"
      width="460px"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      :show-close="false"
    >
      <el-alert type="error" :closable="false" class="settings-alert" title="恢复码只显示这一次">
        请立刻抄写或存入密码管理器；旧恢复码已全部作废。离开本弹窗后无法再次查看。
      </el-alert>

      <ul class="regen-codes">
        <li v-for="code in recoveryCodes" :key="code" class="regen-code">{{ code }}</li>
      </ul>

      <el-button :icon="DocumentCopy" class="regen-copy-all" @click="copyRecoveryCodes">
        复制全部恢复码
      </el-button>

      <el-checkbox v-model="savedAcknowledged" class="regen-ack">我已保存恢复码</el-checkbox>

      <template #footer>
        <el-button type="primary" :disabled="!savedAcknowledged" @click="closeCodes">
          关闭
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
// 恢复码重新生成入口(设置页 MFA 区块)。两步交互对应后端
// POST /api/me/mfa/regenerate-recovery:先用第二因子二次确认,再展示新码。
// 展示段刻意照搬 MFAEnroll.vue 的恢复码交互(一次性提示 + 复制全部 + 必须勾选
// "我已保存" 才放行),两处对"恢复码只显示这一次"的口径必须一致。
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { DocumentCopy } from '@element-plus/icons-vue'
import { regenerateRecoveryCodes } from '@/api/mfa'
import { regenerateErrorMessage } from './recovery-regen-utils'

const confirmVisible = ref(false)
const codesVisible = ref(false)
const submitting = ref(false)
const confirmCode = ref('')
const recoveryCodes = ref<string[]>([])
const savedAcknowledged = ref(false)

const openConfirm = () => {
  confirmCode.value = ''
  confirmVisible.value = true
}

const submitRegenerate = async () => {
  const code = confirmCode.value.trim()
  // 双保险:按钮已 disabled,这里再挡一次(回车提交会绕过 disabled)。
  if (code === '') {
    ElMessage.warning('请输入动态码或恢复码')
    return
  }
  submitting.value = true
  try {
    const data = await regenerateRecoveryCodes(code)
    recoveryCodes.value = data.recovery_codes ?? []
    // 只在拿到新码后才切窗:失败必须停在确认框里让用户换码重试。
    confirmVisible.value = false
    savedAcknowledged.value = false
    codesVisible.value = true
  } catch (err) {
    ElMessage.error(regenerateErrorMessage(err))
    // 码是一次性的:验证失败就清空,免得用户对着过期码反复点。
    confirmCode.value = ''
  } finally {
    submitting.value = false
  }
}

const copyRecoveryCodes = async () => {
  try {
    await navigator.clipboard.writeText(recoveryCodes.value.join('\n'))
    ElMessage.success('恢复码已复制')
  } catch {
    ElMessage.warning('复制失败，请手动选择复制')
  }
}

// closeCodes 关闭前清空明文:弹窗关掉后组件仍存活,留着等于把一次性凭证
// 挂在内存里等下一个使用者。
const closeCodes = () => {
  if (!savedAcknowledged.value) return
  codesVisible.value = false
  recoveryCodes.value = []
  confirmCode.value = ''
}

// 暴露给组件测试(script-setup 绑定默认不外露)。
defineExpose({ confirmVisible, codesVisible, recoveryCodes, savedAcknowledged, submitRegenerate })
</script>

<style scoped>
.settings-alert {
  margin-bottom: var(--ph-space-4);
}
.regen-hint {
  margin: 0 0 var(--ph-space-3);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
  line-height: 1.6;
}
.regen-codes {
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
.regen-code {
  font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 13px;
  letter-spacing: 1px;
  text-align: center;
  color: var(--ph-text-primary);
  user-select: all;
}
.regen-copy-all {
  width: 100%;
  border-radius: var(--ph-radius);
}
.regen-ack {
  display: flex;
  margin: var(--ph-space-4) 0 0;
}
</style>
