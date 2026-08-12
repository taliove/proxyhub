<template>
  <AuthShell subtitle="系统初始化" wide>
    <el-steps :active="step" finish-status="success" align-center>
      <el-step title="管理员账户" />
      <el-step title="安全策略" />
      <el-step title="完成" />
    </el-steps>

    <div class="step-content">
      <!-- 步骤 1: 管理员账户 -->
      <div v-if="step === 0">
        <el-form :model="form" label-width="120px">
          <el-alert type="warning" :closable="false" class="step-alert">
            为安全起见，禁止使用 "admin" 作为用户名
          </el-alert>
          <el-form-item label="用户名">
            <el-input v-model="form.username" placeholder="请输入用户名（非 admin）" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input
              v-model="form.password"
              type="password"
              placeholder="至少 8 位"
              show-password
            />
          </el-form-item>
          <el-form-item label="确认密码">
            <el-input
              v-model="form.confirmPassword"
              type="password"
              placeholder="再次输入密码"
              show-password
            />
          </el-form-item>
        </el-form>
      </div>

      <!-- 步骤 2: 安全策略 -->
      <div v-if="step === 1">
        <el-form :model="form" label-width="180px">
          <el-form-item label="登录失败封禁阈值">
            <el-input-number v-model="form.banThreshold" :min="3" :max="10" />
            <span class="form-tip">连续失败此次数后封禁 IP</span>
          </el-form-item>
          <el-form-item label="封禁时长（小时）">
            <el-input-number v-model="form.banDuration" :min="1" :max="24" />
          </el-form-item>
        </el-form>
      </div>

      <!-- 步骤 3: 完成 -->
      <div v-if="step === 2">
        <el-result icon="success" title="初始化完成！">
          <template #sub-title>
            <p>系统已完成初始化，即将跳转到登录页面</p>
          </template>
        </el-result>
      </div>
    </div>

    <div class="step-actions">
      <el-button v-if="step > 0 && step < 2" @click="step--">上一步</el-button>
      <el-button v-if="step < 2" type="primary" @click="nextStep">下一步</el-button>
    </div>
  </AuthShell>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import AuthShell from '@/components/AuthShell.vue'
import client from '@/api/client'

const router = useRouter()
const step = ref(0)

const form = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  banThreshold: 5,
  banDuration: 1
})

const nextStep = async () => {
  if (step.value === 0) {
    if (form.username === 'admin') {
      ElMessage.error('禁止使用 admin 作为用户名')
      return
    }
    if (!form.username || form.username.length < 3) {
      ElMessage.error('用户名至少 3 位')
      return
    }
    if (form.password.length < 8) {
      ElMessage.error('密码至少 8 位')
      return
    }
    if (form.password !== form.confirmPassword) {
      ElMessage.error('两次密码不一致')
      return
    }
    step.value++
  } else if (step.value === 1) {
    try {
      await client.post('/setup', {
        username: form.username,
        password: form.password,
        security: {
          ban_threshold: form.banThreshold,
          ban_duration: `${form.banDuration}h`
        }
      })
      step.value++
      setTimeout(() => router.push('/login'), 2000)
    } catch {
      ElMessage.error('初始化失败')
    }
  }
}
</script>

<style scoped>
.step-alert {
  margin-bottom: var(--ph-space-5);
}

.step-content {
  margin: var(--ph-space-7) 0;
  min-height: 300px;
}

.step-actions {
  text-align: center;
  margin-top: var(--ph-space-6);
}

.form-tip {
  margin-left: var(--ph-space-2);
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-xs);
}
</style>
