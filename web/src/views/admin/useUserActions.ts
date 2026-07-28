// 用户管理页的行内动作(确认框 + 接口调用 + 提示),从 Users.vue 抽出:
// 每个动作都是"弹确认 -> 打接口 -> 提示/刷新"的同构三段,聚在一处便于核对
// 确认文案是否把后果说清(禁用/删除/重置密码/清空受信 IP/重置 MFA 都不可撤销)。
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import type { AdminUser } from '@/api/users'
import {
  disableUser,
  enableUser,
  deleteUser,
  resetUserPassword,
  resetUserMFA,
  switchUser
} from '@/api/users'
import { clearUserTrustedIPs } from '@/api/trusted-ips'

/**
 * useUserActions 组装行内动作。reload 由调用方提供(列表重新拉取),
 * 动作只负责触发,不持有列表状态。
 */
export function useUserActions(reload: () => Promise<void>) {
  const router = useRouter()
  const authStore = useAuthStore()

  // 重置密码的结果:新密码只在这一次响应里出现,弹窗关掉即不可再看。
  const passwordResult = ref('')
  const passwordResultVisible = ref(false)

  async function onDisable(row: AdminUser) {
    await ElMessageBox.confirm(
      `确定禁用用户「${row.username}」吗?禁用后该用户无法登录,已建资源保留。`,
      '禁用确认',
      { type: 'warning', confirmButtonText: '禁用', cancelButtonText: '取消' }
    )
    await disableUser(row.id)
    ElMessage.success('已禁用')
    await reload()
  }

  async function onEnable(row: AdminUser) {
    await enableUser(row.id)
    ElMessage.success('已启用')
    await reload()
  }

  async function onDelete(row: AdminUser) {
    await ElMessageBox.confirm(
      `确定删除用户「${row.username}」吗?该用户的机场、节点、订阅地址将一并删除,不可恢复。`,
      '删除确认',
      { type: 'error', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
    await deleteUser(row.id)
    ElMessage.success('已删除')
    await reload()
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

  // onClearTrustedIPs 抹掉目标的全部受信 IP 授权(ticket 10),让该用户所有地址
  // 下次登录都要重新过 MFA。用于设备/网络疑似失陷。
  async function onClearTrustedIPs(row: AdminUser) {
    await ElMessageBox.confirm(
      `确定清空用户「${row.username}」的受信 IP 吗?清空后该用户所有地址下次登录都需要重新完成 MFA 验证。`,
      '清空受信 IP 确认',
      { type: 'warning', confirmButtonText: '清空', cancelButtonText: '取消' }
    )
    const res = await clearUserTrustedIPs(row.id)
    ElMessage.success(`已清空 ${res.removed} 条受信 IP`)
  }

  // onResetMFA 是用户同时丢了认证器与恢复码时的运维出口:解绑并作废恢复码,
  // 账号回到未绑定态,下次请求被 requireMFAEnrolled 推回绑定流程。
  // 对用户的第二因子是破坏性操作,确认文案必须把两个后果都写明。
  async function onResetMFA(row: AdminUser) {
    await ElMessageBox.confirm(
      `确定重置用户「${row.username}」的 MFA 吗?将解除该用户的 TOTP 绑定并作废其恢复码,下次登录需重新绑定验证器。`,
      '重置 MFA 确认',
      { type: 'warning', confirmButtonText: '重置', cancelButtonText: '取消' }
    )
    await resetUserMFA(row.id)
    ElMessage.success(`已重置「${row.username}」的 MFA,该用户下次登录需重新绑定`)
  }

  // onEnterSpace 切换到目标用户的空间(ticket 09)。服务端把 acting_user_id 落到
  // 会话上,前端镜像一份让导航栏出横幅,再整页重载让每个视图以被代理身份重取。
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

  return {
    passwordResult,
    passwordResultVisible,
    onDisable,
    onEnable,
    onDelete,
    onResetPassword,
    onClearTrustedIPs,
    onResetMFA,
    onEnterSpace
  }
}
