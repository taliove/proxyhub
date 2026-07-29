<template>
  <el-card class="rules-card">
    <template #header>
      <div class="card-header">
        <span>IP 规则</span>
        <el-button link @click="load">刷新</el-button>
      </div>
    </template>

    <el-alert
      type="info"
      :closable="false"
      class="rules-alert"
      title="整站拒止：该来源访问任何页面与接口都被拒;拉取黑名单：只掐订阅地址拉取,管理面不受影响。重复提交同一条规则会顺延到期时间。"
    />

    <!-- 新增规则 -->
    <div class="rule-form">
      <el-input
        v-model="form.target"
        placeholder="IP 或 CIDR,如 203.0.113.10 / 203.0.113.0/24"
        class="ctl-target"
        clearable
      />
      <el-select v-model="form.scope" placeholder="作用范围" class="ctl-scope">
        <el-option
          v-for="opt in RULE_SCOPE_OPTIONS"
          :key="opt.value"
          :label="opt.label"
          :value="opt.value"
        />
      </el-select>
      <el-select v-model="form.duration" placeholder="时长" class="ctl-duration">
        <el-option
          v-for="opt in RULE_DURATION_OPTIONS"
          :key="opt.value"
          :label="opt.label"
          :value="opt.value"
        />
      </el-select>
      <el-input v-model="form.comment" placeholder="备注（可选）" class="ctl-comment" clearable />
      <el-button type="primary" :loading="saving" @click="onCreate">新增规则</el-button>
    </div>

    <el-table v-loading="loading" :data="rules" size="small">
      <el-table-column prop="ip_or_cidr" label="IP / CIDR" min-width="170" />
      <el-table-column label="作用范围" width="120">
        <template #default="{ row }">
          <el-tag :type="ruleScopeTag(row.scope)" size="small">{{
            ruleScopeLabel(row.scope)
          }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="来源" width="90">
        <template #default="{ row }">
          <el-tag :type="ruleSourceTag(row.source)" size="small" effect="plain">{{
            ruleSourceLabel(row.source)
          }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="到期" min-width="180">
        <template #default="{ row }">
          <span v-if="row.permanent">永久</span>
          <span v-else>{{ formatTime(row.expires_at) }}</span>
          <el-tag v-if="row.expired" size="small" type="info" effect="plain" class="expired-tag">
            已失效
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="comment" label="备注" min-width="140" show-overflow-tooltip />
      <el-table-column label="操作" width="150">
        <template #default="{ row }">
          <el-button
            v-if="row.scope === 'sub'"
            link
            type="warning"
            size="small"
            @click="onPromote(row)"
            >升级整站</el-button
          >
          <el-button link type="danger" size="small" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && rules.length === 0" description="暂无 IP 规则" :image-size="60" />
  </el-card>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { listIPRules, createIPRule, deleteIPRule, promoteIPRule, type IPRule } from '@/api/ip-rules'
import {
  RULE_DURATION_OPTIONS,
  RULE_SCOPE_OPTIONS,
  ruleScopeLabel,
  ruleScopeTag,
  ruleSourceLabel,
  ruleSourceTag
} from '@/utils/pullguard'

// IP 规则管理区块(pull-guard ticket 06):列表 + 新增 + 删除 + sub->global 升级。
// 每次写操作后重新拉取列表:规则是安全判定,UI 不能凭本地状态假装写成功
// (重复新增会顺延到期时间,只有真实读取才能反映实际窗口)。

const rules = ref<IPRule[]>([])
const loading = ref(false)
const saving = ref(false)

// 默认拉取黑名单 + 24 小时:比整站拒止/永久保守,误操作代价最小。
const emptyForm = () => ({ target: '', scope: 'sub', duration: '24h', comment: '' })
const form = reactive(emptyForm())

const load = async () => {
  loading.value = true
  try {
    const data = await listIPRules()
    rules.value = data.rules || []
  } finally {
    loading.value = false
  }
}

const onCreate = async () => {
  const target = form.target.trim()
  if (!target) {
    ElMessage.warning('请填写 IP 或 CIDR')
    return
  }
  saving.value = true
  try {
    await createIPRule({
      ip_or_cidr: target,
      scope: form.scope,
      duration: form.duration,
      comment: form.comment.trim()
    })
    ElMessage.success('规则已生效')
    Object.assign(form, emptyForm())
    await load()
  } finally {
    saving.value = false
  }
}

const onDelete = async (row: IPRule) => {
  await deleteIPRule(row.id)
  ElMessage.success('规则已删除')
  await load()
}

const onPromote = async (row: IPRule) => {
  await promoteIPRule(row.id)
  ElMessage.success('已升级为整站拒止')
  await load()
}

// permanent 的行走"永久"文案,不会走到这里;expires_at 缺失时兜一个占位符。
const formatTime = (t: string | null) => (t ? new Date(t).toLocaleString('zh-CN') : '-')

// 暴露给组件测试驱动表单(与 Audit.test.ts 直接读写 vm 的既有模式一致)。
defineExpose({ load, form })

onMounted(load)
</script>

<style scoped>
.rules-card {
  margin-top: var(--ph-space-5);
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.rules-alert {
  margin-bottom: var(--ph-space-4);
}
.rule-form {
  display: flex;
  flex-wrap: wrap;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-4);
}
.ctl-target {
  width: 260px;
}
.ctl-scope {
  width: 140px;
}
.ctl-duration {
  width: 120px;
}
.ctl-comment {
  width: 200px;
}
/* 失效徽标不参与收缩,到期时间过长时优先截断时间文字 */
.expired-tag {
  margin-left: var(--ph-space-2);
  flex: none;
}
</style>
