<template>
  <div class="trusted-ip-list">
    <el-alert
      type="info"
      :closable="false"
      class="settings-alert"
      :title="`受信 IP 在 ${ttlDays} 天内免二次验证;撤销后该 IP 下次登录重新要求 MFA。同一地址 30 天内 MFA 成功登录达 ${threshold} 次会出现在推荐列表。`"
    />

    <div class="auto-trust">
      <span class="auto-trust-label">自动信任</span>
      <el-switch v-model="autoTrust" :loading="savingAuto" @change="onToggleAuto" />
      <span class="hint">
        开启后,达阈值的来源地址在下次 MFA
        登录成功时自动进入受信列表;关闭则只出现在推荐列表,需手动采纳。
      </span>
    </div>

    <h4 class="section-title">当前受信 IP</h4>
    <el-table v-loading="loading" :data="trusted" size="small">
      <el-table-column prop="ip" label="IP" min-width="150" />
      <el-table-column label="地理位置" min-width="120">
        <template #default="{ row }">{{ formatGeo(row) }}</template>
      </el-table-column>
      <el-table-column label="最后使用" min-width="170">
        <template #default="{ row }">{{ formatTime(row.last_used_at) }}</template>
      </el-table-column>
      <el-table-column label="到期" min-width="170">
        <template #default="{ row }">
          <span>{{ formatTime(row.expires_at) }}</span>
          <el-tag v-if="row.expired" size="small" type="danger" effect="plain" class="expired-tag">
            已过期
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button link type="danger" size="small" @click="onRevoke(row.ip)">撤销</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-empty v-if="!loading && trusted.length === 0" description="暂无受信 IP" :image-size="60" />

    <h4 class="section-title">信任推荐</h4>
    <el-table v-loading="loading" :data="recommendations" size="small">
      <el-table-column prop="ip" label="IP" min-width="150" />
      <el-table-column label="地理位置" min-width="120">
        <template #default="{ row }">{{ formatGeo(row) }}</template>
      </el-table-column>
      <el-table-column prop="mfa_successes" label="30 天 MFA 成功" min-width="130" />
      <el-table-column label="操作" width="100">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="onAdopt(row.ip)">一键信任</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-empty
      v-if="!loading && recommendations.length === 0"
      description="暂无推荐(达阈值的来源地址会出现在这里)"
      :image-size="60"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import {
  listTrustedIPs,
  trustIP,
  revokeTrustedIP,
  setAutoTrustIP,
  type TrustedIP,
  type TrustRecommendation
} from '@/api/trusted-ips'

// 受信 IP 管理(ticket 10):列表(含地理/最后使用/到期)+ 撤销 + 推荐一键采纳 +
// 自动信任开关。所有写操作后重新拉取,避免本地状态与后端判定漂移
// (采纳后该地址从推荐移入受信,靠一次真实读取表达)。

// ttlDays 与后端 store.TrustedIPTTL(30 天)对齐,仅用于文案。
const ttlDays = 30

const loading = ref(false)
const savingAuto = ref(false)
const trusted = ref<TrustedIP[]>([])
const recommendations = ref<TrustRecommendation[]>([])
const autoTrust = ref(false)
const threshold = ref(3)

const load = async () => {
  loading.value = true
  try {
    const data = await listTrustedIPs()
    trusted.value = data.trusted || []
    recommendations.value = data.recommendations || []
    autoTrust.value = data.auto_trust_ip === true
    if (data.threshold > 0) threshold.value = data.threshold
  } finally {
    loading.value = false
  }
}

const onRevoke = async (ip: string) => {
  await revokeTrustedIP(ip)
  ElMessage.success('已撤销,该 IP 下次登录需重新验证')
  await load()
}

const onAdopt = async (ip: string) => {
  await trustIP(ip)
  ElMessage.success('已加入受信列表')
  await load()
}

// onToggleAuto 开关失败时回滚本地值:开关是安全决策,UI 不能显示成功而后端仍是关。
const onToggleAuto = async (value: boolean) => {
  savingAuto.value = true
  try {
    await setAutoTrustIP(value)
    ElMessage.success(value ? '已开启自动信任' : '已关闭自动信任')
  } catch {
    autoTrust.value = !value
  } finally {
    savingAuto.value = false
  }
}

const formatTime = (t: string) => {
  if (!t) return '-'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '-' : d.toLocaleString('zh-CN')
}

// formatGeo 离线库无记录时(私有/保留网段)返回「未知」,不因缺地理信息留白。
const formatGeo = (row: { region_name?: string; region_code?: string }) =>
  row.region_name || row.region_code || '未知'

onMounted(load)
</script>

<style scoped>
.section-title {
  margin: var(--ph-space-4) 0 var(--ph-space-2);
  font-size: var(--ph-text-sm);
  color: var(--ph-text-secondary);
}
.hint {
  display: block;
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
}
.settings-alert {
  margin-bottom: var(--ph-space-4);
}
.expired-tag {
  margin-left: var(--ph-space-2);
}
.auto-trust {
  margin-bottom: var(--ph-space-2);
}
.auto-trust-label {
  margin-right: var(--ph-space-2);
  font-size: var(--ph-text-sm);
}
</style>
