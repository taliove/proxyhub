<template>
  <div>
    <PageHeader />
    <el-card>
      <el-tabs>
        <el-tab-pane v-if="authStore.isSuperAdmin" label="安全设置">
          <el-form :model="settings" label-width="180px" class="settings-form">
            <el-form-item label="登录失败封禁阈值">
              <el-input-number v-model="settings.ban_threshold" :min="3" :max="10" />
            </el-form-item>
            <el-form-item label="封禁时长">
              <el-input v-model="settings.ban_duration" placeholder="1h" />
            </el-form-item>
            <el-form-item label="验证码触发次数">
              <el-input-number
                v-model="settings.captcha_trigger_threshold"
                :min="0"
                :max="CAPTCHA_TRIGGER_THRESHOLD_MAX"
                :step="1"
                :precision="0"
              />
              <span class="hint">
                同一 IP 登录失败达该次数后要求验证码。0 = 每次都要求,1 = 一次失败即要求(默认)。
              </span>
            </el-form-item>
            <el-form-item label="订阅拉取限频阈值">
              <el-input-number
                v-model="settings.pull_rate_limit_per_hour"
                :min="0"
                :max="PULL_RATE_LIMIT_MAX"
                :step="1"
                :precision="0"
              />
              <span class="hint">
                单 IP × 单订阅地址每小时允许拉取次数。0 = 关闭限频,默认
                {{ PULL_RATE_LIMIT_DEFAULT }}。
              </span>
            </el-form-item>
            <el-form-item label="自动黑名单升级次数">
              <el-input-number
                v-model="settings.pull_blacklist_escalation_count"
                :min="1"
                :step="1"
                :precision="0"
              />
              <span class="hint">
                1 小时内累计触发限频达该次数后自动拉黑。默认
                {{ PULL_BLACKLIST_ESCALATION_DEFAULT }}。
              </span>
            </el-form-item>
            <el-form-item label="自动黑名单时长">
              <el-input v-model="settings.pull_blacklist_duration" placeholder="24h" />
              <span class="hint">
                自动拉黑规则的有效期,格式如 1h/24h/168h。默认
                {{ PULL_BLACKLIST_DURATION_DEFAULT }}。
              </span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="saveSettings">保存</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>
        <el-tab-pane v-if="authStore.isSuperAdmin" label="告警设置">
          <el-form :model="settings" label-width="180px" class="settings-form">
            <el-form-item label="飞书 Webhook">
              <el-input v-model="settings.feishu_webhook" placeholder="https://..." />
            </el-form-item>
            <el-form-item label="最小可用节点数">
              <el-input-number v-model="settings.min_available_nodes" :min="1" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="saveSettings">保存</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>
        <el-tab-pane label="订阅设置">
          <el-form :model="settings" label-width="180px" class="settings-form">
            <el-form-item label="定时刷新机场">
              <template #label>
                定时刷新机场
                <TenantBadge v-if="!authStore.isSuperAdmin" k="scheduled_refresh_enabled" />
              </template>
              <el-switch
                v-model="settings.scheduled_refresh_enabled"
                active-value="true"
                inactive-value="false"
              />
              <span class="hint">
                默认关闭:机场节点仅由「手动刷新」与粘贴/文件导入更新(机场订阅被服务器侧封锁 403
                是常见现象,定时外打多为无效请求)。开启后按健康检查间隔定时拉取全部启用机场。
              </span>
            </el-form-item>

            <el-form-item v-if="authStore.isSuperAdmin" label="机场拉取并行度">
              <el-input-number v-model="settings.fetch_concurrency" :min="1" :max="10" />
              <span class="hint">
                全量刷新时同时拉取的机场数(1-10,默认 4)。只作用于拉取阶段。
              </span>
            </el-form-item>
            <el-form-item label="地区白名单">
              <RegionWhitelist />
            </el-form-item>
            <el-form-item label="节点名称标准化">
              <template #label>
                节点名称标准化
                <TenantBadge v-if="!authStore.isSuperAdmin" k="standardize_names" />
              </template>
              <el-switch
                v-model="settings.standardize_names"
                active-value="true"
                inactive-value="false"
              />
              <span class="hint">
                开启后,订阅生成时把机场原名统一为标准格式(如 🇭🇰 香港 JS-01)。
              </span>
            </el-form-item>
            <el-form-item v-if="settings.standardize_names === 'true'" label="名称模板">
              <template #label>
                名称模板
                <TenantBadge v-if="!authStore.isSuperAdmin" k="name_template" />
              </template>
              <el-input
                v-model="settings.name_template"
                placeholder="{emoji} {region} {source_abbr}-{index}"
              />
              <span class="hint">
                可用变量：{emoji} {region} {region_code} {source} {source_abbr} {index}
                {original_name}。
              </span>
            </el-form-item>

            <el-form-item label="订阅关键词白名单">
              <template #label>
                订阅关键词白名单
                <TenantBadge v-if="!authStore.isSuperAdmin" k="filter_whitelist" />
              </template>
              <el-input
                v-model="settings.filter_whitelist"
                type="textarea"
                :rows="3"
                placeholder="留空则不启用。非空时,只保留名称命中任一关键词的节点(自建节点豁免)。多个关键词用逗号或换行分隔。"
              />
              <span class="hint"
                >地区白名单优先(按地区代码精确筛选),关键词白名单次之(字符串匹配)。</span
              >
            </el-form-item>
            <el-form-item label="订阅关键词过滤">
              <template #label>
                订阅关键词过滤
                <TenantBadge v-if="!authStore.isSuperAdmin" k="filter_keywords" />
              </template>
              <el-input
                v-model="settings.filter_keywords"
                type="textarea"
                :rows="4"
                placeholder="名称命中任一关键词的节点将被剔除(自建节点豁免)。多个关键词用逗号或换行分隔。"
              />
              <span class="hint">子串匹配、不区分大小写;改动即时对下一次订阅生效。</span>
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="saveSettings">保存</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <el-tab-pane v-if="authStore.isSuperAdmin" label="带宽测试配置">
          <el-form label-width="180px" class="settings-form">
            <el-alert
              type="info"
              :closable="false"
              class="settings-alert"
              title="采用固定时长测速：下行、上行各跑满「测速时长」(默认 10s)。数据量仅作上限。"
            />
            <el-form-item label="测速时长(秒/方向)">
              <el-input v-model="settings.bandwidth_test_duration_sec" placeholder="10" />
            </el-form-item>
            <el-form-item label="下行探测 URL">
              <el-input
                v-model="settings.bandwidth_down_url"
                placeholder="https://speed.cloudflare.com/__down?bytes=1073741824"
              />
            </el-form-item>
            <el-form-item label="上行探测 URL">
              <el-input
                v-model="settings.bandwidth_up_url"
                placeholder="https://speed.cloudflare.com/__up"
              />
            </el-form-item>
            <el-form-item label="上行数据上限(字节)">
              <el-input v-model="settings.bandwidth_up_bytes" placeholder="1073741824 (1GB)" />
            </el-form-item>
            <el-form-item label="单方向硬超时(秒)">
              <el-input v-model="settings.bandwidth_dir_timeout_sec" placeholder="20" />
            </el-form-item>
            <el-form-item label="整体超时(秒)">
              <el-input v-model="settings.bandwidth_timeout_sec" placeholder="60" />
            </el-form-item>
            <el-form-item label="下行合格阈值(Mbps)">
              <el-input v-model="settings.bandwidth_min_down_mbps" placeholder="1.0" />
            </el-form-item>
            <el-form-item label="上行合格阈值(Mbps)">
              <el-input v-model="settings.bandwidth_min_up_mbps" placeholder="1.0" />
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="saveSettings">保存</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <el-tab-pane v-if="authStore.isSuperAdmin" label="直连出口">
          <DirectEgressSettings />
        </el-tab-pane>

        <el-tab-pane label="检测目标配置">
          <DetectionTargets />
        </el-tab-pane>

        <el-tab-pane label="两步验证">
          <h4 class="mfa-section-title">受信 IP</h4>
          <TrustedIPList />
          <h4 class="mfa-section-title">恢复码</h4>
          <RecoveryCodeRegenerate />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { ElMessage, ElTag, ElButton } from 'element-plus'
import {
  getSettings,
  saveSettings as persistSettings,
  MAIN_SETTINGS_KEYS,
  TENANT_SETTINGS_KEYS
} from '@/api/settings'
import { useAuthStore } from '@/stores/auth'
import PageHeader from '@/components/PageHeader.vue'
import RegionWhitelist from '@/components/RegionWhitelist.vue'
import DirectEgressSettings from '@/components/DirectEgressSettings.vue'
import DetectionTargets from '@/components/DetectionTargets.vue'
import TrustedIPList from '@/components/TrustedIPList.vue'
import RecoveryCodeRegenerate from '@/components/RecoveryCodeRegenerate.vue'
import {
  CAPTCHA_TRIGGER_THRESHOLD_DEFAULT,
  CAPTCHA_TRIGGER_THRESHOLD_MAX,
  validateCaptchaTriggerThreshold,
  PULL_RATE_LIMIT_DEFAULT,
  PULL_RATE_LIMIT_MAX,
  validatePullRateLimit,
  PULL_BLACKLIST_ESCALATION_DEFAULT,
  validatePullBlacklistEscalation,
  PULL_BLACKLIST_DURATION_DEFAULT,
  validatePullBlacklistDuration
} from './settings-utils'

const authStore = useAuthStore()

const overridden = ref<Record<string, boolean>>({})

// TenantBadge 租户级键的状态徽标:跟随全局默认 / 已自定义 + 重置。
const TenantBadge = (props: { k: string }) => {
  const isCustom = overridden.value[props.k] === true
  return h('span', { class: 'tenant-badge' }, [
    h(ElTag, { size: 'small', effect: 'plain', type: isCustom ? 'warning' : 'info' }, () =>
      isCustom ? '已自定义' : '跟随全局默认'
    ),
    isCustom
      ? h(
          ElButton,
          { size: 'small', link: true, type: 'danger', onClick: () => resetTenantKey(props.k) },
          () => '重置'
        )
      : null
  ])
}
TenantBadge.props = ['k']

const resetTenantKey = async (k: string) => {
  await persistSettings({}, [k])
  const { settings: s, overridden: o } = await getSettings()
  Object.assign(settings.value, pickKeys(s, TENANT_SETTINGS_KEYS))
  overridden.value = o
  ElMessage.success('已重置为跟随全局默认')
}

const pickKeys = (src: Record<string, string>, keys: readonly string[]) =>
  Object.fromEntries(keys.filter((k) => k in src).map((k) => [k, src[k]]))

// 主保存 payload 只含主表单自己的键(MAIN_SETTINGS_KEYS 白名单)
const settings = ref<Record<string, string | number>>({
  ban_threshold: 5,
  ban_duration: '1h',
  captcha_trigger_threshold: CAPTCHA_TRIGGER_THRESHOLD_DEFAULT,
  feishu_webhook: '',
  min_available_nodes: 10,
  scheduled_refresh_enabled: 'false',
  fetch_concurrency: 4,
  filter_keywords: '',
  filter_whitelist: '',
  standardize_names: 'false',
  name_template: '',
  bandwidth_down_url: '',
  bandwidth_up_url: '',
  bandwidth_up_bytes: '',
  bandwidth_test_duration_sec: '',
  bandwidth_timeout_sec: '',
  bandwidth_dir_timeout_sec: '',
  bandwidth_min_down_mbps: '',
  bandwidth_min_up_mbps: '',
  pull_rate_limit_per_hour: PULL_RATE_LIMIT_DEFAULT,
  pull_blacklist_escalation_count: PULL_BLACKLIST_ESCALATION_DEFAULT,
  pull_blacklist_duration: PULL_BLACKLIST_DURATION_DEFAULT
})

onMounted(async () => {
  const { settings: s, overridden: o } = await getSettings()
  Object.assign(settings.value, s)
  overridden.value = o
})

const saveSettings = async () => {
  // 输入校验先行(超管专属字段)
  if (authStore.isSuperAdmin) {
    const err = validateCaptchaTriggerThreshold(settings.value.captcha_trigger_threshold)
    if (err) {
      ElMessage.error(err)
      return
    }
    const pullRateErr = validatePullRateLimit(settings.value.pull_rate_limit_per_hour)
    if (pullRateErr) {
      ElMessage.error(pullRateErr)
      return
    }
    const pullEscalationErr = validatePullBlacklistEscalation(
      settings.value.pull_blacklist_escalation_count
    )
    if (pullEscalationErr) {
      ElMessage.error(pullEscalationErr)
      return
    }
    const pullDurationErr = validatePullBlacklistDuration(settings.value.pull_blacklist_duration)
    if (pullDurationErr) {
      ElMessage.error(pullDurationErr)
      return
    }
  }
  // 统一序列化为字符串
  const keys = authStore.isSuperAdmin ? MAIN_SETTINGS_KEYS : TENANT_SETTINGS_KEYS
  const payload = Object.fromEntries(keys.map((k) => [k, String(settings.value[k])]))
  await persistSettings(payload)
  const { overridden: o } = await getSettings()
  overridden.value = o
  ElMessage.success('保存成功')
}
</script>

<style scoped>
.settings-form {
  max-width: 680px;
}
.settings-alert {
  margin-bottom: var(--ph-space-4);
}
.hint {
  display: block;
  margin-top: var(--ph-space-1);
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
  line-height: 1.5;
}
.form-extra {
  font-size: var(--ph-text-xs);
  color: var(--ph-text-secondary);
}
.target-toolbar {
  display: flex;
  gap: var(--ph-space-2);
  margin-bottom: var(--ph-space-4);
}
.target-actions {
  margin-top: var(--ph-space-4);
}
.mfa-section-title {
  margin: var(--ph-space-5) 0 var(--ph-space-3);
  font-size: var(--ph-text-base);
  color: var(--ph-text-primary);
}
.mfa-section-title:first-child {
  margin-top: 0;
}
.tenant-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
  margin-left: var(--ph-space-2);
  vertical-align: middle;
}
</style>
