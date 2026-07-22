<template>
  <el-form :model="form" label-width="180px" class="direct-egress-form">
    <el-alert
      type="info"
      :closable="false"
      class="settings-alert"
      title="本机运行 TUN 代理(Clash Verge/TAG 等 fake-ip 模式)时,保证节点检测结果可信;绕过失败时检测会报错(严格模式);关闭则恢复系统网络栈。只作用于检测链路,改后立即对后续检测生效,无需重启。"
    />
    <el-form-item label="启用直连出口">
      <el-switch v-model="form.direct_egress_enabled" active-value="true" inactive-value="false" />
      <span class="hint">
        开启后,检测连接经自带 DoH 解析真实 IP 并绑定物理网卡,绕过本机 TUN 劫持;绕过失败(DoH
        解析失败/网卡绑定失败)时检测会报错,不会悄悄退化为系统拨号。
      </span>
    </el-form-item>
    <el-form-item label="DoH 端点" :error="dohUrlError">
      <el-input
        v-model="form.direct_egress_doh_url"
        placeholder="https://223.5.5.5/dns-query"
        @input="dohUrlError = ''"
      />
      <template #extra>
        <span class="form-extra">
          须为 http(s) URL,host 用 IP 字面量(如 223.5.5.5 或 [2606:4700:4700::1111], 域名会被 TUN
          劫持);留空用默认值 https://223.5.5.5/dns-query
        </span>
      </template>
    </el-form-item>
    <el-form-item label="物理网卡名">
      <el-input v-model="form.direct_egress_interface" placeholder="留空自动识别,如 en0 / eth0" />
      <template #extra>
        <span class="form-extra">
          留空=自动识别物理网卡;识别失败时检测会报错,可在此显式指定(如 en0)
        </span>
      </template>
    </el-form-item>
    <el-form-item>
      <el-button type="primary" :loading="saving" @click="save">保存</el-button>
    </el-form-item>
  </el-form>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { getSettings, saveSettings, DIRECT_EGRESS_DEFAULTS } from '@/api/settings'
import type { DirectEgressSettings } from '@/types'

// 直连出口(见 CONTEXT.md「直连出口」;ticket 0021)独立配置区:
// 后端 /api/settings 为 map[string]string 且按键合并(INSERT OR REPLACE),
// 故本组件只读写自己的三个键,与设置页其他分区互不干扰。
const form = ref<DirectEgressSettings>({ ...DIRECT_EGRESS_DEFAULTS })
const saving = ref(false)
// DoH 端点校验错误(保存前校验,输入时清空)
const dohUrlError = ref('')

onMounted(async () => {
  try {
    const data = await getSettings()
    form.value = {
      direct_egress_enabled:
        data.direct_egress_enabled ?? DIRECT_EGRESS_DEFAULTS.direct_egress_enabled,
      direct_egress_doh_url:
        data.direct_egress_doh_url ?? DIRECT_EGRESS_DEFAULTS.direct_egress_doh_url,
      direct_egress_interface:
        data.direct_egress_interface ?? DIRECT_EGRESS_DEFAULTS.direct_egress_interface
    }
  } catch (e) {
    ElMessage.error(`加载直连出口设置失败: ${e instanceof Error ? e.message : String(e)}`)
  }
})

// 与后端构造期约束对齐(newDoHResolver):host 必须是 IP 字面量,
// 域名 host 需二次解析,会落入被劫持的系统 DNS。
const IPV4_RE = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/
const isIpLiteralHost = (hostname: string): boolean => {
  const m = IPV4_RE.exec(hostname)
  if (m) return m.slice(1).every((octet) => Number(octet) <= 255)
  // URL 规范:IPv6 字面量 host 带方括号,如 [2606:4700:4700::1111]
  return /^\[[0-9a-fA-F:.]+\]$/.test(hostname)
}

const isHttpUrl = (raw: string): boolean => {
  try {
    const u = new URL(raw)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

const save = async () => {
  const url = form.value.direct_egress_doh_url.trim()
  if (url && !isHttpUrl(url)) {
    dohUrlError.value = 'DoH 端点须为 http(s) URL,如 https://223.5.5.5/dns-query'
    return
  }
  if (url && !isIpLiteralHost(new URL(url).hostname)) {
    dohUrlError.value =
      'DoH 端点 host 须为 IP 字面量(如 223.5.5.5 或 [2606:4700:4700::1111]),域名会被 TUN 劫持'
    return
  }
  dohUrlError.value = ''
  saving.value = true
  try {
    await saveSettings({ ...form.value })
    ElMessage.success('保存成功')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
/* 表单测量宽度收敛:长表单不铺满整页,控件对齐更利于扫读 */
.direct-egress-form {
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
</style>
