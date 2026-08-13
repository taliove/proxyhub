<template>
  <!-- 代为重置订阅链接(issue #117):列出该用户全部订阅地址,逐条重置;
       旧链接默认 3 天宽限。结果弹窗内嵌,父页只持有 v-model 与 user -->
  <el-dialog v-model="visible" :title="`订阅链接 - ${user?.username ?? ''}`" width="640px">
    <el-table v-loading="loading" :data="userEndpoints" row-key="id">
      <el-table-column prop="alias" label="别名" />
      <el-table-column label="订阅 URL">
        <template #default="{ row }">
          <span class="url-text">{{ subscriptionUrl(row) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="110">
        <template #default="{ row }">
          <el-button
            link
            type="warning"
            :loading="resettingId === row.id"
            @click="onResetLink(row)"
          >
            重置链接
          </el-button>
        </template>
      </el-table-column>
      <template #empty><p class="muted">该用户还没有订阅地址。</p></template>
    </el-table>
    <template #footer>
      <el-button @click="visible = false">关闭</el-button>
    </template>

    <!-- 代重置结果:新链接只展示一次 -->
    <el-dialog v-model="resultVisible" title="订阅链接已重置" width="560px" append-to-body>
      <p class="form-hint">
        新链接已生成并转交用户。旧链接在宽限期(至 {{ resetResult?.grace_expires_at }}
        UTC)内仍可使用:
      </p>
      <el-input :model-value="resultUrl" readonly>
        <template #append>
          <el-button @click="copyResult">复制</el-button>
        </template>
      </el-input>
      <template #footer>
        <el-button type="primary" @click="resultVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { AdminUser } from '@/api/users'
import type { Endpoint } from '@/types'
import { adminListUserEndpoints, adminResetEndpointLink } from '@/api/users'
import { copyPassword } from '@/utils/useradmin'

const visible = defineModel<boolean>({ required: true })

const props = defineProps<{
  user: AdminUser | null
}>()

const loading = ref(false)
const userEndpoints = ref<Endpoint[]>([])
const resettingId = ref<number | null>(null)
const resultVisible = ref(false)
const resetResult = ref<Endpoint | null>(null)
const resultUrl = ref('')

const subscriptionUrl = (ep: Endpoint) =>
  `${window.location.origin}/sub/${ep.path}?token=${ep.token}`

// 打开时按目标用户拉取;关闭即清空,下次打开重新拉(URL 可能已被重置轮换)
watch(
  visible,
  async (open) => {
    if (!open || !props.user) return
    loading.value = true
    try {
      userEndpoints.value = await adminListUserEndpoints(props.user.id)
    } catch (err) {
      ElMessage.error(`加载订阅地址失败:${err instanceof Error ? err.message : String(err)}`)
      userEndpoints.value = []
    } finally {
      loading.value = false
    }
  },
  { immediate: true }
)

const onResetLink = async (ep: Endpoint) => {
  await ElMessageBox.confirm(
    `重置订阅地址「${ep.alias}」的链接后,旧链接进入 3 天宽限期,宽限期后使用该链接的设备将无法更新订阅。端点配置全部保留。`,
    '代为重置订阅链接',
    { type: 'warning', confirmButtonText: '确认重置', cancelButtonText: '取消' }
  )
  resettingId.value = ep.id
  try {
    const fresh = await adminResetEndpointLink(props.user!.id, ep.id)
    resetResult.value = fresh
    resultUrl.value = subscriptionUrl(fresh)
    resultVisible.value = true
    // 同步列表里的行(URL 已轮换)
    userEndpoints.value = userEndpoints.value.map((e) => (e.id === fresh.id ? fresh : e))
  } catch (err) {
    ElMessage.error(`重置失败:${err instanceof Error ? err.message : String(err)}`)
  } finally {
    resettingId.value = null
  }
}

const copyResult = () => copyPassword(resultUrl.value)
</script>

<style scoped>
.url-text {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  word-break: break-all;
}
.muted {
  color: var(--ph-text-secondary);
}
.form-hint {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.6;
  margin: 0 0 var(--ph-space-3);
}
</style>
