<template>
  <el-card>
    <template #header>系统设置</template>
    <el-tabs>
      <el-tab-pane label="安全设置">
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

      <el-tab-pane label="告警设置">
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

          <!-- 地区白名单（新增） -->
          <el-form-item label="地区白名单">
            <RegionWhitelist />
          </el-form-item>

          <!-- 节点名称标准化（见 ADR 0012） -->
          <el-form-item label="节点名称标准化">
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
      <el-tab-pane label="带宽测试配置">
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

      <!-- 检测目标配置 -->
      <el-tab-pane label="检测目标配置">
        <div class="target-toolbar">
          <el-button type="primary" @click="addTarget">添加目标</el-button>
          <el-button @click="loadTargets">刷新</el-button>
        </div>
        <el-table :data="detectionTargets" border>
          <el-table-column prop="name" label="名称" width="120">
            <template #default="{ row }">
              <el-input v-model="row.name" size="small" />
            </template>
          </el-table-column>
          <el-table-column prop="kind" label="类型" width="130">
            <template #default="{ row }">
              <el-tag size="small" :type="row.kind && row.kind !== 'generic' ? 'warning' : 'info'">
                {{ row.kind && row.kind !== 'generic' ? row.kind : 'generic' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="url" label="URL" min-width="200">
            <template #default="{ row }">
              <el-input v-model="row.url" size="small" />
            </template>
          </el-table-column>
          <el-table-column prop="method" label="方法" width="80">
            <template #default="{ row }">
              <el-select v-model="row.method" size="small">
                <el-option label="GET" value="GET" />
                <el-option label="POST" value="POST" />
              </el-select>
            </template>
          </el-table-column>
          <el-table-column prop="expect_status" label="期望状态码" width="150">
            <template #default="{ row }">
              <el-input v-model="row.expect_status_str" size="small" placeholder="200,204" />
            </template>
          </el-table-column>
          <el-table-column prop="response_excludes" label="排除关键字" width="180">
            <template #default="{ row }">
              <el-input v-model="row.response_excludes_str" size="small" placeholder="逗号分隔" />
            </template>
          </el-table-column>
          <el-table-column label="操作" width="100">
            <template #default="{ $index }">
              <el-button type="danger" size="small" link @click="removeTarget($index)"
                >删除</el-button
              >
            </template>
          </el-table-column>
        </el-table>
        <div class="target-actions">
          <el-button type="primary" @click="saveTargets">保存配置</el-button>
        </div>
      </el-tab-pane>
    </el-tabs>
  </el-card>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import client from '@/api/client'
import RegionWhitelist from '@/components/RegionWhitelist.vue'

const settings = ref({
  ban_threshold: 5,
  ban_duration: '1h',
  feishu_webhook: '',
  min_available_nodes: 10,
  // 订阅设置:字符串取值以匹配后端 map[string]string 契约
  scheduled_refresh_enabled: 'true',
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

// 检测目标配置
interface DetectionTarget {
  name: string
  // 检测类型:空/generic=通用判定,其余(netflix 等)=专用解锁判定。
  // UI 只读展示并原样回传,避免保存时丢失播种目标的 kind。
  kind?: string
  url: string
  method: string
  headers: Record<string, string>
  expect_status: number[]
  response_contains: string[]
  response_excludes: string[]
  // UI 辅助字段(数组转逗号字符串)
  expect_status_str?: string
  response_excludes_str?: string
}

const detectionTargets = ref<DetectionTarget[]>([])

onMounted(async () => {
  const data = await client.get('/settings')
  Object.assign(settings.value, data)
  await loadTargets()
})

const saveSettings = async () => {
  // 后端 /settings 解码为 map[string]string,数字/布尔值会导致 400。
  // 统一序列化为字符串,兼容 el-input-number(数字)与 el-switch(字符串)取值。
  const payload = Object.fromEntries(Object.entries(settings.value).map(([k, v]) => [k, String(v)]))
  await client.post('/settings', payload)
  ElMessage.success('保存成功')
}

const loadTargets = async () => {
  const data = await client.get<unknown, DetectionTarget[]>('/settings/detection-targets')
  // 转换数组为字符串(便于编辑)
  detectionTargets.value = data.map((t: DetectionTarget) => ({
    ...t,
    headers: t.headers || {},
    expect_status_str: (t.expect_status || []).join(','),
    response_excludes_str: (t.response_excludes || []).join(',')
  }))
}

const saveTargets = async () => {
  // 转换字符串回数组
  const payload = detectionTargets.value.map((t) => ({
    name: t.name,
    // 原样回传 kind(专用解锁目标),缺省交由后端按 generic 处理
    ...(t.kind ? { kind: t.kind } : {}),
    url: t.url,
    method: t.method,
    headers: t.headers || {},
    expect_status: (t.expect_status_str || '')
      .split(',')
      .map((s) => parseInt(s.trim()))
      .filter((n) => !isNaN(n)),
    response_contains: t.response_contains || [],
    response_excludes: (t.response_excludes_str || '')
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s)
  }))
  await client.put('/settings/detection-targets', payload)
  ElMessage.success('保存成功')
  await loadTargets()
}

const addTarget = () => {
  detectionTargets.value.push({
    name: '',
    url: '',
    method: 'GET',
    headers: {},
    expect_status: [200],
    response_contains: [],
    response_excludes: [],
    expect_status_str: '200',
    response_excludes_str: ''
  })
}

const removeTarget = (index: number) => {
  detectionTargets.value.splice(index, 1)
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
</style>
