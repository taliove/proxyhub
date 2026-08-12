<template>
  <div class="batch-bar">
    <span class="muted num">已选 {{ count }} 项</span>

    <!-- 屏蔽可逆,用 warning 不用 danger(危险色纪律);屏蔽仅对机场节点有意义,blockableCount=0 时禁用。 -->
    <el-button type="warning" size="small" :disabled="blockableCount === 0" @click="emit('block')">
      屏蔽选中
    </el-button>
    <el-button size="small" :disabled="blockableCount === 0" @click="emit('unblock')">
      取消屏蔽
    </el-button>

    <el-divider direction="vertical" />

    <!-- 主入口「补齐信息」= 深度体检全量(CONTEXT「检查动作」);三个子集动作收进「高级」下拉。
         0 勾选禁用;任一动作运行中禁用其余动作(串行,避免争用检测会话)。 -->
    <el-button
      type="primary"
      size="small"
      :disabled="count === 0 || anyRunning"
      @click="emit('start', 'exam')"
    >
      补齐信息
    </el-button>
    <el-dropdown trigger="click" @command="(id: BatchActionId) => emit('start', id)">
      <el-button size="small" :disabled="count === 0 || anyRunning">
        高级
        <el-icon class="el-icon--right"><ArrowDown /></el-icon>
      </el-button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item v-for="a in subActions" :key="a.id" :command="a.id">
            {{ a.label }}
          </el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>

    <el-divider direction="vertical" />

    <!-- 页面级与低频命令收进「更多」:刷新名称 / 按机场屏蔽 / 清空机场节点 / 清理失败节点。
         清空/清理是不可逆感操作,标危险色。 -->
    <el-dropdown trigger="click" @command="(cmd: string) => emit('more-command', cmd)">
      <el-button size="small">
        更多
        <el-icon class="el-icon--right"><ArrowDown /></el-icon>
      </el-button>
      <template #dropdown>
        <el-dropdown-menu>
          <el-dropdown-item command="refresh-names">刷新名称</el-dropdown-item>
          <el-dropdown-item command="block-source">按机场屏蔽</el-dropdown-item>
          <el-dropdown-item command="purge-airport" divided>
            <span class="danger-item">清空机场节点</span>
          </el-dropdown-item>
          <el-dropdown-item command="cleanup">
            <span class="danger-item">清理失败节点</span>
          </el-dropdown-item>
        </el-dropdown-menu>
      </template>
    </el-dropdown>

    <!-- 进度:运行中的动作显示 x/N 与取消(4 动作互斥,至多一个在跑)。 -->
    <span v-if="runningAction" class="muted num action-progress">
      {{ runningAction.label }} {{ runningAction.state.completed }}/{{ runningAction.state.total }}
      <el-button link type="warning" size="small" @click="emit('cancel', runningAction.id)">
        取消
      </el-button>
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { ArrowDown } from '@element-plus/icons-vue'

// 上下文批量栏:仅当选中数 > 0 时由装配层渲染。承载 4 个检查动作(作用于勾选集、全部任务化)
// 与既有勾选操作(屏蔽/取消屏蔽/刷新名称),以及页面级非检查命令(「更多」菜单)。
// 4 动作词汇与单节点面同名同义(见 CONTEXT「检查动作」)。进度复用 jobs 轮询(完成 x/N,可取消)。

// BatchActionId 检查动作标识;exam = 主入口「补齐信息」(backfill),exam-full = 高级「深度体检」。
export type BatchActionId = 'detect' | 'stability' | 'speedtest' | 'exam' | 'exam-full'

// BatchActionState 单个动作的运行态与进度。
export interface BatchActionState {
  running: boolean
  completed: number
  total: number
}

// BatchAction 批量栏渲染用的动作描述(装配层注入 label 与实时 state)。
export interface BatchAction {
  id: BatchActionId
  label: string
  state: BatchActionState
}

const props = defineProps<{
  count: number
  blockableCount: number
  actions: BatchAction[]
}>()

const emit = defineEmits<{
  (e: 'block'): void
  (e: 'unblock'): void
  (e: 'start', id: BatchActionId): void
  (e: 'cancel', id: BatchActionId): void
  (e: 'more-command', cmd: string): void
}>()

// 任一动作运行中:禁用全部动作按钮(串行心智模型,避免并发争用)。
const anyRunning = computed(() => props.actions.some((a) => a.state.running))
// 当前运行中的动作(至多一个),用于显示进度与取消。
const runningAction = computed(() => props.actions.find((a) => a.state.running) ?? null)
// 「高级」下拉 = 三个子集动作(主入口 exam 不在其中,避免重复)
const subActions = computed(() => props.actions.filter((a) => a.id !== 'exam'))
</script>

<style scoped>
.batch-bar {
  display: flex;
  align-items: center;
  gap: var(--ph-space-2);
  flex-wrap: wrap;
  margin-bottom: var(--ph-space-3);
  padding: var(--ph-space-2) var(--ph-space-3);
  background: var(--ph-bg-hover);
  border-radius: var(--ph-radius);
}
.muted {
  color: var(--ph-text-secondary);
  font-size: var(--ph-text-sm);
}
.num {
  font-variant-numeric: tabular-nums;
}
.action-progress {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-2);
}
.danger-item {
  color: var(--ph-danger);
}
</style>
