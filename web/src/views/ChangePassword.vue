<template>
  <AuthShell subtitle="修改密码">
    <el-alert
      v-if="authStore.mustChangePassword"
      type="warning"
      :closable="false"
      class="change-password__alert"
      title="首次登录请修改密码"
    />

    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      class="change-password__form"
      label-position="top"
      @submit.prevent="handleSubmit"
    >
      <el-form-item label="旧密码" prop="oldPassword">
        <el-input
          v-model="form.oldPassword"
          type="password"
          placeholder="请输入当前密码"
          size="large"
          autocomplete="current-password"
          show-password
        />
      </el-form-item>
      <el-form-item label="新密码" prop="newPassword">
        <el-input
          v-model="form.newPassword"
          type="password"
          placeholder="至少 8 位，需同时含字母与数字"
          size="large"
          autocomplete="new-password"
          show-password
        />
      </el-form-item>
      <el-form-item label="确认新密码" prop="confirmPassword">
        <el-input
          v-model="form.confirmPassword"
          type="password"
          placeholder="再次输入新密码"
          size="large"
          autocomplete="new-password"
          show-password
          @keyup.enter="handleSubmit"
        />
      </el-form-item>
      <el-button
        type="primary"
        native-type="submit"
        :loading="loading"
        class="change-password__submit"
        size="large"
      >
        {{ loading ? '提交中……' : '确认修改' }}
      </el-button>
    </el-form>

    <p class="change-password__footer">修改成功后将退出登录，请使用新密码重新登录</p>
  </AuthShell>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import AuthShell from '@/components/AuthShell.vue'
import client from '@/api/client'

const router = useRouter()
const authStore = useAuthStore()

const loading = ref(false)
const formRef = ref<FormInstance>()

const form = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

// 与后端 validateNewPassword 对齐:>= 8 位且同时含字母与数字。
// 前端只做体验层面的早期反馈,真正的校验权威在服务端。
const validateNewPassword = (_rule: unknown, value: string, callback: (e?: Error) => void) => {
  if (!value) {
    callback(new Error('请输入新密码'))
    return
  }
  if (value.length < 8) {
    callback(new Error('新密码至少 8 位'))
    return
  }
  if (!/[A-Za-z]/.test(value) || !/\d/.test(value)) {
    callback(new Error('新密码需同时包含字母与数字'))
    return
  }
  callback()
}

const validateConfirm = (_rule: unknown, value: string, callback: (e?: Error) => void) => {
  if (!value) {
    callback(new Error('请再次输入新密码'))
    return
  }
  if (value !== form.newPassword) {
    callback(new Error('两次输入的新密码不一致'))
    return
  }
  callback()
}

const rules: FormRules = {
  oldPassword: [{ required: true, message: '请输入旧密码', trigger: 'blur' }],
  newPassword: [{ validator: validateNewPassword, trigger: 'blur' }],
  confirmPassword: [{ validator: validateConfirm, trigger: 'blur' }]
}

const handleSubmit = async () => {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  loading.value = true
  try {
    await client.post('/me/password', {
      old_password: form.oldPassword,
      new_password: form.newPassword
    })
    // 后端已销毁会话并清除 must_change_password;本地同步清位,守卫即刻放行,
    // 之后任何业务请求 401,由 axios 拦截器统一送回登录页。
    authStore.clearMustChangePassword()
    ElMessage.success('密码已修改，请重新登录')
    router.push('/')
  } catch {
    // 错误由 axios 拦截器统一提示
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
/* ---------- 提示 ---------- */
.change-password__alert {
  margin-bottom: var(--ph-space-5);
}

/* ---------- 表单 ---------- */
.change-password__form :deep(.el-form-item) {
  margin-bottom: 20px;
}
.change-password__form :deep(.el-input__wrapper) {
  border-radius: var(--ph-radius);
  padding: 4px 12px;
}
.change-password__submit {
  width: 100%;
  margin-top: var(--ph-space-1);
  font-weight: 600;
  border-radius: var(--ph-radius);
}

.change-password__footer {
  margin: var(--ph-space-5) 0 0;
  text-align: center;
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
</style>
