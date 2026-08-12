<template>
  <div class="ph-navbar">
    <div class="ph-navbar__left">
      <!-- 原生 button 承载图标动作:键盘可聚焦、Enter/Space 可触发(critique P2 无障碍) -->
      <button
        type="button"
        class="ph-navbar__toggle"
        :aria-label="collapsed ? '展开侧边栏' : '折叠侧边栏'"
        @click="emit('toggle')"
      >
        <el-icon>
          <IconLayoutSidebarLeftCollapse v-if="!collapsed" :size="26" />
          <IconLayoutSidebarLeftExpand v-else :size="26" />
        </el-icon>
      </button>
    </div>

    <div class="ph-navbar__right">
      <!-- Impersonation banner (ticket 09): shown while the super admin is
           inside another user's space; exit returns to their own scope. -->
      <div v-if="authStore.actingUser" class="ph-navbar__acting" data-testid="acting-banner">
        <el-tag type="warning" size="small" effect="dark">
          正在查看：{{ authStore.actingUser.username }}
        </el-tag>
        <el-button link type="warning" size="small" class="ph-navbar__exit" @click="onExitSwitch">
          退出
        </el-button>
      </div>

      <el-tooltip :content="isDark ? '切换亮色' : '切换暗色'" placement="bottom">
        <button
          type="button"
          class="ph-navbar__action"
          :aria-label="isDark ? '切换亮色' : '切换暗色'"
          @click="layout.toggleDark()"
        >
          <el-icon>
            <IconMoon v-if="!isDark" :size="22" />
            <IconSun v-else :size="22" />
          </el-icon>
        </button>
      </el-tooltip>

      <el-tooltip content="全屏" placement="bottom">
        <button type="button" class="ph-navbar__action" aria-label="全屏" @click="toggleFullscreen">
          <el-icon>
            <IconMaximize :size="22" />
          </el-icon>
        </button>
      </el-tooltip>

      <el-dropdown @command="onCommand">
        <span class="ph-navbar__user">
          <el-icon><IconUser :size="18" /></el-icon>
          <span class="ph-navbar__username">{{ authStore.username }}</span>
          <el-icon><IconChevronDown :size="16" /></el-icon>
        </span>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item command="logout">
              <el-icon><IconLogout :size="16" /></el-icon> 退出登录
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  IconLayoutSidebarLeftCollapse,
  IconLayoutSidebarLeftExpand,
  IconMoon,
  IconSun,
  IconMaximize,
  IconUser,
  IconChevronDown,
  IconLogout
} from '@tabler/icons-vue'
import { useLayoutStore } from '@/stores/layout'
import { useAuthStore } from '@/stores/auth'
import { exitSwitch } from '@/api/users'
import client from '@/api/client'

defineProps<{ collapsed: boolean }>()
const emit = defineEmits<{ toggle: [] }>()

const router = useRouter()
const layout = useLayoutStore()
const authStore = useAuthStore()

const isDark = computed(() => layout.isDark)

function toggleFullscreen(): void {
  // requestFullscreen/exitFullscreen 返回 Promise 且异步 reject,需用 .catch 捕获
  const action = !document.fullscreenElement
    ? document.documentElement.requestFullscreen()
    : document.exitFullscreen()
  action?.catch(() => ElMessage.warning('当前浏览器不支持全屏或已被拒绝'))
}

// onExitSwitch leaves the impersonated user space and returns to the
// super admin's own scope; the page is reloaded so every view re-fetches
// data under the correct user id.
async function onExitSwitch(): Promise<void> {
  try {
    await exitSwitch()
    authStore.setActingUser(null)
    ElMessage.success('已退出用户空间')
    router.push('/').then(() => router.go(0))
  } catch {
    ElMessage.error('退出失败')
  }
}

async function onCommand(command: string): Promise<void> {
  if (command === 'logout') {
    try {
      await client.post('/logout')
    } catch {
      // 即便退出接口失败也清理本地状态并跳转
    }
    authStore.clearAuth()
    ElMessage.success('已退出')
    router.push('/login')
  }
}
</script>

<style scoped>
.ph-navbar {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
}

.ph-navbar__left,
.ph-navbar__right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.ph-navbar__toggle,
.ph-navbar__action {
  /* 原生 button 重置 + 图标盒子;font-size 定的是图标尺寸(1em 盒),不是文本 */
  background: none;
  border: none;
  font-family: inherit;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 22px;
  cursor: pointer;
  color: var(--ph-text-regular);
  padding: 8px;
  border-radius: var(--ph-radius-sm);
  transition:
    background-color var(--ph-transition),
    color var(--ph-transition);
}

/* 折叠/展开是导航主开关,视觉上要比其他动作重一档 */
.ph-navbar__toggle {
  font-size: 26px;
  color: var(--ph-text-primary);
}

.ph-navbar__toggle:hover,
.ph-navbar__action:hover {
  background: var(--ph-bg-hover);
  color: var(--ph-primary);
}

/* 键盘焦点环与主色一致(仪器焦点环) */
.ph-navbar__toggle:focus-visible,
.ph-navbar__action:focus-visible {
  outline: 2px solid var(--ph-primary);
  outline-offset: 1px;
}

.ph-navbar__user {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  padding: 6px 10px;
  border-radius: var(--ph-radius-sm);
  color: var(--ph-text-regular);
  outline: none;
  transition: background-color var(--ph-transition);
}

.ph-navbar__user:hover {
  background: var(--ph-bg-hover);
}

.ph-navbar__username {
  font-size: 14px;
}

/* Impersonation banner: warning tone so the admin cannot miss they're
   acting on behalf of another user (ticket 09). */
.ph-navbar__acting {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px;
  border-radius: var(--ph-radius-sm);
  background: var(--ph-bg-hover);
}

.ph-navbar__exit {
  padding: 0;
  height: auto;
  line-height: 1;
}
</style>
