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
                关闭后仅「手动刷新」会拉取机场。注意:关闭并重启后节点池为空,订阅将暂时返回
                503,需手动刷新一次(见 ADR 0004)。
              </span>
            </el-form-item>

            <el-form-item v-if="authStore.isSuperAdmin" label="机场拉取并行度">
              <el-input-number v-model="settings.fetch_concurrency" :min="1" :max="10" />
              <span class="hint">
                全量刷新时同时拉取的机场数(1-10,默认 4)。只作用于拉取阶段;健康检查并发独立配置。
              </span>
            </el-form-item>

            <!-- 地区白名单（新增） -->
            <el-form-item label="地区白名单">
              <RegionWhitelist />
            </el-form-item>

            <!-- 节点名称标准化（见 ADR 0012） -->
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
                开启后,订阅生成时把机场原名统一为标准格式(如 🇭🇰 香港
                JS-01);关闭则保留机场原名。机场简称在「机场管理」中配置。
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
                可用变量:{emoji}(国旗) {region}(地区中文) {region_code}(地区代码) {source}(机场全名)
                {source_abbr}(机场简称) {index}(序号) {original_name}(原名)。留空用默认模板。
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
                placeholder="留空则不启用。非空时,只保留名称命中任一关键词的机场节点(自建节点豁免)。多个关键词用逗号或换行分隔,如:香港,新加坡,美国,日本"
              />
              <span class="hint"
                >地区白名单优先(按地区代码精确筛选),关键词白名单次之(字符串匹配)。子串匹配、不区分大小写(见
                ADR 0009)。</span
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
                placeholder="名称命中任一关键词的机场节点将在订阅生成时被剔除(自建节点豁免)。多个关键词用逗号或换行分隔,如:剩余流量,官网,到期"
              />
              <span class="hint"
                >子串匹配、不区分大小写;改动即时对下一次订阅生效,无需刷新(见 ADR 0005)。</span
              >
            </el-form-item>
            <el-form-item>
              <el-button type="primary" @click="saveSettings">保存</el-button>
            </el-form-item>
          </el-form>
        </el-tab-pane>

        <!-- 带宽测试配置 -->
        <el-tab-pane v-if="authStore.isSuperAdmin" label="带宽测试配置">
          <el-form label-width="180px" class="settings-form">
            <el-alert
              type="info"
              :closable="false"
              class="settings-alert"
              title="采用固定时长测速:下行、上行各跑满「测速时长」(默认 10s),两条曲线等长。数据量仅作上限,须足够大以免快节点提前传完。留空用系统默认值。"
            />
            <el-form-item label="测速时长(秒/方向)">
              <el-input v-model="settings.bandwidth_test_duration_sec" placeholder="10" />
              <template #extra>
                <span class="form-extra">
                  下行/上行各自跑满这个时长,速率 = 该时长内传输字节 / 时长。两个方向相同 → 曲线等长
                </span>
              </template>
            </el-form-item>
            <el-form-item label="下行探测 URL">
              <el-input
                v-model="settings.bandwidth_down_url"
                placeholder="https://speed.cloudflare.com/__down?bytes=1073741824"
              />
              <template #extra>
                <span class="form-extra">
                  下行数据上限由 URL 的 bytes= 参数控制(默认 1GB);读完仍未到时长会自动续传
                </span>
              </template>
            </el-form-item>
            <el-form-item label="上行探测 URL">
              <el-input
                v-model="settings.bandwidth_up_url"
                placeholder="https://speed.cloudflare.com/__up"
              />
            </el-form-item>
            <el-form-item label="上行数据上限(字节)">
              <el-input v-model="settings.bandwidth_up_bytes" placeholder="1073741824 (1GB)" />
              <template #extra>
                <span class="form-extra">
                  上行在测速时长内持续发送的数据上限;到时长即停(通常用不满)
                </span>
              </template>
            </el-form-item>
            <el-form-item label="单方向硬超时(秒)">
              <el-input v-model="settings.bandwidth_dir_timeout_sec" placeholder="20" />
              <template #extra>
                <span class="form-extra">
                  防链路卡死的硬上限;正常应先到「测速时长」自然结束,此值仅兜底(应大于测速时长)
                </span>
              </template>
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

        <!-- 直连出口(见 CONTEXT.md「直连出口」;ticket 0021):碰本机网络出口,超管专属 -->
        <el-tab-pane v-if="authStore.isSuperAdmin" label="直连出口">
          <DirectEgressSettings />
        </el-tab-pane>

        <!-- 检测目标配置(每租户,独立组件) -->
        <el-tab-pane label="检测目标配置">
          <DetectionTargets />
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

const authStore = useAuthStore()

// overridden 每键覆盖标记(仅普通用户视角有真值;超管编辑全局默认,恒为空)。
const overridden = ref<Record<string, boolean>>({})

// TenantBadge 租户级键的状态徽标(本地渲染组件):跟随全局默认 / 已自定义 + 重置。
// 重置 = 删除本人覆盖,回到跟随全局默认(立即生效,不等保存)。
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

// resetTenantKey 删除某租户级键的本人覆盖并刷新生效视图。
const resetTenantKey = async (k: string) => {
  await persistSettings({}, [k])
  const { settings: s, overridden: o } = await getSettings()
  Object.assign(settings.value, pickKeys(s, TENANT_SETTINGS_KEYS))
  overridden.value = o
  ElMessage.success('已重置为跟随全局默认')
}

// pickKeys 从信封 settings 里挑出指定键(普通用户视角后端只回租户级键,
// 超管视角是全量键;挑键防止其他 tab 的值被 Object.assign 吞进主表单)。
const pickKeys = (src: Record<string, string>, keys: readonly string[]) =>
  Object.fromEntries(keys.filter((k) => k in src).map((k) => [k, src[k]]))

// 主保存 payload 只含主表单自己的键(白名单见 MAIN_SETTINGS_KEYS):
// onMounted 的 Object.assign 会吞进全量键(含 direct_egress_* 等其他 tab 的键),
// 全量回写会把挂载时的旧值静默覆盖其他 tab 刚保存的新值;各 tab 只写自己的键。
const settings = ref<Record<string, string | number>>({
  ban_threshold: 5,
  ban_duration: '1h',
  feishu_webhook: '',
  min_available_nodes: 10,
  // 订阅设置:字符串取值以匹配后端 map[string]string 契约
  scheduled_refresh_enabled: 'true',
  // 机场拉取并行度(el-input-number 数字,保存时统一序列化为字符串;后端 1-10 clamp,默认 4)
  fetch_concurrency: 4,
  filter_keywords: '',
  filter_whitelist: '',
  // 节点名称标准化(见 ADR 0012)
  standardize_names: 'false',
  name_template: '',
  // 带宽测试配置(缺省用后端默认值)
  bandwidth_down_url: '',
  bandwidth_up_url: '',
  bandwidth_up_bytes: '',
  bandwidth_test_duration_sec: '',
  bandwidth_timeout_sec: '',
  bandwidth_dir_timeout_sec: '',
  bandwidth_min_down_mbps: '',
  bandwidth_min_up_mbps: ''
})

onMounted(async () => {
  const { settings: s, overridden: o } = await getSettings()
  Object.assign(settings.value, s)
  overridden.value = o
})

const saveSettings = async () => {
  // 统一序列化为字符串,兼容 el-input-number(数字)与 el-switch(字符串)取值。
  // 超管写全局默认(主表单全键);普通用户只写租户级键(落本人 user_settings),
  // 超管专属键即使夹带也会被后端忽略,但这里直接不发,语义更干净。
  const keys = authStore.isSuperAdmin ? MAIN_SETTINGS_KEYS : TENANT_SETTINGS_KEYS
  const payload = Object.fromEntries(keys.map((k) => [k, String(settings.value[k])]))
  await persistSettings(payload)
  // 保存后刷新覆盖标记(新写的键变为已自定义)
  const { overridden: o } = await getSettings()
  overridden.value = o
  ElMessage.success('保存成功')
}
</script>

<style scoped>
/* 表单测量宽度收敛：长表单不铺满整页，控件对齐更利于扫读 */
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
.tenant-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--ph-space-1);
  margin-left: var(--ph-space-2);
  vertical-align: middle;
}
</style>
