<template>
  <div>
    <PageHeader>
      <el-button type="primary" @click="openCreateDialog">新建用户</el-button>
    </PageHeader>

    <el-card>
      <el-table v-loading="loading" :data="users" row-key="id">
        <el-table-column prop="username" label="用户名" min-width="120" />
        <el-table-column label="角色" width="110">
          <template #default="{ row }">
            <el-tag :type="row.role === 'super_admin' ? 'warning' : 'info'" size="small">
              {{ row.role === 'super_admin' ? '超管' : '普通用户' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="机场配额" width="110">
          <template #default="{ row }">
            <span class="num">{{ row.airport_count }}/{{ row.quota?.max_airports ?? '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="订阅配额" width="110">
          <template #default="{ row }">
            <span class="num">{{ row.endpoint_count }}/{{ row.quota?.max_endpoints ?? '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="Xray 端口段" width="140">
          <template #default="{ row }">
            <span v-if="row.quota" class="num">
              {{ row.quota.xray_port_start }}-{{ row.quota.xray_port_end }}
            </span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.disabled ? 'info' : 'success'" size="small">
              {{ row.disabled ? '禁用' : '启用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="最近登录" width="170">
          <template #default="{ row }">
            <span v-if="row.last_login_at" class="num">{{ formatTime(row.last_login_at) }}</span>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <!-- Row actions are hidden for super_admin: the system must always keep
             at least one manageable admin account. Disabled users cannot be
             entered (server returns 409), so the enter-space action is hidden too. -->
        <el-table-column label="操作" width="340">
          <template #default="{ row }">
            <template v-if="row.role !== 'super_admin'">
              <el-button v-if="!row.disabled" link type="primary" @click="onEnterSpace(row)">
                进入空间
              </el-button>
              <el-button link type="primary" @click="openEditDialog(row)">编辑</el-button>
              <el-button v-if="row.disabled" link type="success" @click="onEnable(row)">
                启用
              </el-button>
              <el-button v-else link type="warning" @click="onDisable(row)">禁用</el-button>
              <el-button link type="primary" @click="onResetPassword(row)">重置密码</el-button>
              <el-button link type="danger" @click="onDelete(row)">删除</el-button>
            </template>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- Create user: password is auto-generated and can be copied -->
    <el-dialog v-model="createVisible" title="新建用户" width="520px">
      <el-form label-width="110px">
        <el-form-item label="用户名">
          <el-input v-model="createForm.username" placeholder="登录用户名" />
          <div class="form-hint">admin、root、guest 等系统保留名不可用</div>
        </el-form-item>
        <el-form-item label="初始密码">
          <el-input v-model="createForm.password" readonly class="pw-append">
            <template #append>
              <el-button @click="regeneratePassword">换一换</el-button>
              <el-button @click="copyPassword(createForm.password)">复制</el-button>
            </template>
          </el-input>
          <div class="form-hint">自动生成的随机密码,请复制后转交用户;首次登录需改密</div>
        </el-form-item>
        <el-form-item label="机场配额">
          <el-input-number v-model="createForm.quota.max_airports" :min="0" />
        </el-form-item>
        <el-form-item label="订阅配额">
          <el-input-number v-model="createForm.quota.max_endpoints" :min="0" />
        </el-form-item>
        <el-form-item label="Xray 端口段">
          <div class="port-range">
            <el-input-number v-model="createForm.quota.xray_port_start" :min="0" :max="65535" />
            <span class="muted">-</span>
            <el-input-number v-model="createForm.quota.xray_port_end" :min="0" :max="65535" />
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- Edit quota / role -->
    <el-dialog v-model="editVisible" title="编辑用户" width="520px">
      <el-form label-width="110px">
        <el-form-item label="用户名">
          <el-input :model-value="editForm.username" disabled />
        </el-form-item>
        <el-form-item label="机场配额">
          <el-input-number v-model="editForm.max_airports" :min="0" />
        </el-form-item>
        <el-form-item label="订阅配额">
          <el-input-number v-model="editForm.max_endpoints" :min="0" />
        </el-form-item>
        <el-form-item label="Xray 端口段">
          <div class="port-range">
            <el-input-number v-model="editForm.xray_port_start" :min="0" :max="65535" />
            <span class="muted">-</span>
            <el-input-number v-model="editForm.xray_port_end" :min="0" :max="65535" />
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="editVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitEdit">保存</el-button>
      </template>
    </el-dialog>

    <!-- Reset-password result: shown once, must be copied now -->
    <el-dialog v-model="passwordResultVisible" title="密码已重置" width="460px">
      <p class="form-hint">请将新密码复制并转交用户,关闭后无法再次查看:</p>
      <el-input :model-value="passwordResult" readonly>
        <template #append>
          <el-button @click="copyPassword(passwordResult)">复制</el-button>
        </template>
      </el-input>
      <template #footer>
        <el-button type="primary" @click="passwordResultVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import { useAuthStore } from '@/stores/auth'
import { copyPassword, generatePassword, toastCreateUserError } from '@/utils/useradmin'
import type { AdminUser } from '@/api/users'
import {
  listUsers,
  createUser,
  updateUser,
  disableUser,
  enableUser,
  deleteUser,
  resetUserPassword,
  switchUser
} from '@/api/users'

const router = useRouter()
const authStore = useAuthStore()

const loading = ref(false)
const submitting = ref(false)
const users = ref<AdminUser[]>([])

const createVisible = ref(false)
const editVisible = ref(false)
const passwordResultVisible = ref(false)
const passwordResult = ref('')

// createForm holds the create-dialog state; password is auto-generated.
const createForm = ref({
  username: '',
  password: '',
  quota: { max_airports: 5, max_endpoints: 10, xray_port_start: 20000, xray_port_end: 20010 }
})

// editForm mirrors the row being edited; id < 0 means "no selection".
const editForm = ref({
  id: -1,
  username: '',
  max_airports: 0,
  max_endpoints: 0,
  xray_port_start: 0,
  xray_port_end: 0
})

function formatTime(iso: string): string {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleString()
}

async function load() {
  loading.value = true
  try {
    // The backend returns a bare array (no envelope).
    users.value = await listUsers()
  } finally {
    loading.value = false
  }
}

function openCreateDialog() {
  createForm.value = {
    username: '',
    password: generatePassword(),
    quota: { max_airports: 5, max_endpoints: 10, xray_port_start: 20000, xray_port_end: 20010 }
  }
  createVisible.value = true
}

function regeneratePassword() {
  createForm.value = { ...createForm.value, password: generatePassword() }
}

async function submitCreate() {
  if (!createForm.value.username.trim()) {
    ElMessage.warning('请输入用户名')
    return
  }
  submitting.value = true
  try {
    // Backend adminCreateUserRequest takes quota fields flat, not nested.
    await createUser({
      username: createForm.value.username.trim(),
      password: createForm.value.password,
      max_airports: createForm.value.quota.max_airports,
      max_endpoints: createForm.value.quota.max_endpoints,
      xray_port_start: createForm.value.quota.xray_port_start,
      xray_port_end: createForm.value.quota.xray_port_end
    })
    ElMessage.success('创建成功')
    createVisible.value = false
    await load()
  } catch (err) {
    // Dialog stays open so the name can be fixed in place.
    toastCreateUserError(err, createForm.value.username.trim())
  } finally {
    submitting.value = false
  }
}

function openEditDialog(row: AdminUser) {
  editForm.value = {
    id: row.id,
    username: row.username,
    // quota is null for users without a quota row (e.g. the seeded super admin).
    max_airports: row.quota?.max_airports ?? 0,
    max_endpoints: row.quota?.max_endpoints ?? 0,
    xray_port_start: row.quota?.xray_port_start ?? 0,
    xray_port_end: row.quota?.xray_port_end ?? 0
  }
  editVisible.value = true
}

async function submitEdit() {
  submitting.value = true
  try {
    await updateUser(editForm.value.id, {
      max_airports: editForm.value.max_airports,
      max_endpoints: editForm.value.max_endpoints,
      xray_port_start: editForm.value.xray_port_start,
      xray_port_end: editForm.value.xray_port_end
    })
    ElMessage.success('保存成功')
    editVisible.value = false
    await load()
  } finally {
    submitting.value = false
  }
}

async function onDisable(row: AdminUser) {
  await ElMessageBox.confirm(
    `确定禁用用户「${row.username}」吗?禁用后该用户无法登录,已建资源保留。`,
    '禁用确认',
    { type: 'warning', confirmButtonText: '禁用', cancelButtonText: '取消' }
  )
  await disableUser(row.id)
  ElMessage.success('已禁用')
  await load()
}

async function onEnable(row: AdminUser) {
  await enableUser(row.id)
  ElMessage.success('已启用')
  await load()
}

async function onDelete(row: AdminUser) {
  await ElMessageBox.confirm(
    `确定删除用户「${row.username}」吗?该用户的机场、节点、订阅地址将一并删除,不可恢复。`,
    '删除确认',
    { type: 'error', confirmButtonText: '删除', cancelButtonText: '取消' }
  )
  await deleteUser(row.id)
  ElMessage.success('已删除')
  await load()
}

async function onResetPassword(row: AdminUser) {
  await ElMessageBox.confirm(
    `确定重置用户「${row.username}」的密码吗?重置后旧密码立即失效,用户需用新密码登录并改密。`,
    '重置密码确认',
    { type: 'warning', confirmButtonText: '重置', cancelButtonText: '取消' }
  )
  const res = await resetUserPassword(row.id)
  passwordResult.value = res.password
  passwordResultVisible.value = true
}

// onEnterSpace switches the admin into the target user's space (ticket 09).
// The server persists acting_user_id on the session; we mirror it in the
// auth store so the navbar shows the banner, then reload so every view
// re-fetches under the impersonated identity.
async function onEnterSpace(row: AdminUser) {
  await ElMessageBox.confirm(
    `确定进入用户「${row.username}」的空间吗?之后所有页面将以该用户身份展示与操作。`,
    '进入用户空间',
    { type: 'warning', confirmButtonText: '进入', cancelButtonText: '取消' }
  )
  await switchUser(row.id)
  authStore.setActingUser({ id: row.id, username: row.username })
  ElMessage.success(`已进入「${row.username}」的空间`)
  router.push('/').then(() => router.go(0))
}

onMounted(load)

// Exposed for component tests (script-setup bindings stay internal otherwise).
defineExpose({
  createForm,
  editForm,
  passwordResult,
  passwordResultVisible,
  submitCreate,
  submitEdit,
  onEnterSpace
})
</script>

<style scoped>
.num {
  font-variant-numeric: tabular-nums;
}
.muted {
  color: var(--ph-text-placeholder);
}
.form-hint {
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
}
.port-range {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
}
/* element-plus sizes a SINGLE append button with -20px margins so it bleeds
   into the container's 0/20px padding. Two buttons in one append slot then
   overlap each other by 40px; neutralize the bleed and draw our own divider. */
.pw-append :deep(.el-input-group__append) {
  padding: 0;
}
.pw-append :deep(.el-input-group__append .el-button) {
  margin: 0;
  border-radius: 0;
}
.pw-append :deep(.el-input-group__append .el-button + .el-button) {
  border-left: var(--el-border);
}
</style>
