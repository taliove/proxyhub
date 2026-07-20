<template>
  <div>
    <el-card>
      <template #header>
        <div style="display: flex; justify-content: space-between">
          <span>自建节点</span>
          <div>
            <el-button @click="openImportDialog">一键导入</el-button>
            <el-button type="primary" @click="openAddDialog">添加自建节点</el-button>
          </div>
        </div>
      </template>

      <el-alert
        type="info"
        :closable="false"
        style="margin-bottom: 12px"
        title="自建节点作为 FailBack 常驻注入订阅，豁免关键词/白名单/屏蔽等所有过滤。"
      />

      <el-table v-loading="loading" :data="nodes">
        <el-table-column prop="name" label="名称" show-overflow-tooltip />
        <el-table-column prop="protocol" label="协议" width="90" />
        <el-table-column prop="server" label="服务器" show-overflow-tooltip />
        <el-table-column prop="port" label="端口" width="80" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="360">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEditDialog(row)">编辑</el-button>
            <el-button-group size="small" style="margin: 0 8px">
              <el-button :disabled="testing" @click="testNode({ self_node_id: row.id }, 'quick')"
                >快测</el-button
              >
              <el-button :disabled="testing" @click="testNode({ self_node_id: row.id }, 'real')"
                >真实</el-button
              >
              <el-button type="success" @click="onTestCommand(row, 'bandwidth')">带宽</el-button>
            </el-button-group>
            <el-button link @click="toggleNode(row)">{{ row.enabled ? '禁用' : '启用' }}</el-button>
            <el-button link type="danger" @click="deleteNode(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editMode ? '编辑自建节点' : '添加自建节点'"
      width="600px"
    >
      <el-form :model="form" label-width="100px">
        <el-form-item label="名称">
          <el-input v-model="form.name" placeholder="例如：自建香港" />
        </el-form-item>
        <el-form-item label="协议">
          <el-select v-model="form.protocol" style="width: 100%">
            <el-option label="Shadowsocks (ss)" value="ss" />
            <el-option label="Trojan" value="trojan" />
            <el-option label="VMess" value="vmess" />
            <el-option label="VLESS" value="vless" />
          </el-select>
        </el-form-item>
        <el-form-item label="服务器">
          <el-input v-model="form.server" placeholder="域名或 IP" />
        </el-form-item>
        <el-form-item label="端口">
          <el-input-number v-model="form.port" :min="1" :max="65535" />
        </el-form-item>

        <!-- 按协议动态显示字段 -->
        <el-form-item v-if="show('uuid')" label="UUID">
          <el-input v-model="form.uuid" />
        </el-form-item>
        <el-form-item v-if="show('password')" label="密码">
          <el-input v-model="form.password" />
        </el-form-item>
        <el-form-item v-if="show('cipher')" label="加密方式">
          <el-input v-model="form.cipher" placeholder="例如：aes-256-gcm" />
        </el-form-item>
        <el-form-item v-if="show('alter_id')" label="AlterID">
          <el-input-number v-model="form.alter_id" :min="0" />
        </el-form-item>
        <el-form-item v-if="show('network')" label="传输">
          <el-select v-model="form.network" style="width: 100%">
            <el-option label="tcp" value="tcp" />
            <el-option label="ws" value="ws" />
            <el-option label="grpc" value="grpc" />
          </el-select>
        </el-form-item>
        <el-form-item v-if="show('grpc_service_name')" label="gRPC Service">
          <el-input v-model="form.grpc_service_name" placeholder="gRPC service name" />
        </el-form-item>
        <el-form-item v-if="show('tls')" label="TLS">
          <el-switch v-model="form.tls" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" :disabled="submitting" @click="submitForm">
          {{ editMode ? '保存' : '添加' }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 一键导入对话框 -->
    <el-dialog v-model="importDialogVisible" title="一键导入节点" width="600px">
      <el-alert type="info" :closable="false" style="margin-bottom: 16px">
        支持导入 vless://、vmess://、ss://、trojan:// 格式的节点链接
      </el-alert>
      <el-input
        v-model="importUrl"
        type="textarea"
        :rows="6"
        placeholder="粘贴节点链接，例如：vless://uuid@server:port?..."
      />
      <template #footer>
        <el-button @click="importDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="importNode">导入</el-button>
      </template>
    </el-dialog>

    <!-- 带宽测试弹窗(过程态 + 大数字结果) -->
    <BandwidthTestDialog ref="bwDialog" />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { SelfNode } from '@/types'
import client from '@/api/client'
import { useNodeTest } from '@/composables/useNodeTest'
import BandwidthTestDialog from '@/components/BandwidthTestDialog.vue'

const { testing, testNode } = useNodeTest()

// 带宽测试弹窗(过程态 + 大数字结果)
const bwDialog = ref<InstanceType<typeof BandwidthTestDialog> | null>(null)

// onTestCommand 测试下拉:quick/real 走消息提示,bandwidth 走弹窗
const onTestCommand = (row: SelfNode, cmd: string) => {
  if (cmd === 'bandwidth') {
    bwDialog.value?.open({ self_node_id: row.id }, row.name)
    return
  }
  testNode({ self_node_id: row.id }, cmd as 'quick' | 'real')
}

// 每种协议需要的字段（server/port/name 为共有，不在此表）
const PROTOCOL_FIELDS: Record<string, string[]> = {
  ss: ['cipher', 'password'],
  trojan: ['password'],
  vmess: ['uuid', 'alter_id', 'cipher', 'network', 'tls', 'grpc_service_name'],
  vless: ['uuid', 'network', 'tls', 'grpc_service_name']
}

const emptyForm = (): Omit<SelfNode, 'id'> => ({
  name: '',
  protocol: 'ss',
  server: '',
  port: 443,
  uuid: '',
  password: '',
  cipher: '',
  alter_id: 0,
  network: 'tcp',
  tls: false,
  grpc_service_name: '',
  enabled: true
})

const nodes = ref<SelfNode[]>([])
const loading = ref(false)
const dialogVisible = ref(false)
const editMode = ref(false)
const editingId = ref<number | null>(null)
const form = ref(emptyForm())
const importDialogVisible = ref(false)
const importUrl = ref('')
const submitting = ref(false)

const show = (field: string) => {
  // grpc_service_name 仅在 network=grpc 时显示
  if (field === 'grpc_service_name') {
    return (
      form.value.network === 'grpc' &&
      (form.value.protocol === 'vmess' || form.value.protocol === 'vless')
    )
  }
  return (PROTOCOL_FIELDS[form.value.protocol] || []).includes(field)
}

const load = async () => {
  loading.value = true
  try {
    const data = await client.get<any, { nodes: SelfNode[] }>('/self-nodes')
    nodes.value = data.nodes || []
  } finally {
    loading.value = false
  }
}

const openAddDialog = () => {
  editMode.value = false
  editingId.value = null
  form.value = emptyForm()
  dialogVisible.value = true
}

const openEditDialog = (row: SelfNode) => {
  editMode.value = true
  editingId.value = row.id
  form.value = { ...row }
  dialogVisible.value = true
}

const submitForm = async () => {
  if (!form.value.name.trim() || !form.value.server.trim()) {
    ElMessage.warning('名称和服务器不能为空')
    return
  }
  if (submitting.value) {
    return // 防止重复提交
  }
  submitting.value = true
  try {
    if (editMode.value && editingId.value) {
      await client.put(`/self-nodes/${editingId.value}`, form.value)
      ElMessage.success('更新成功')
    } else {
      await client.post('/self-nodes', form.value)
      ElMessage.success('添加成功')
    }
    dialogVisible.value = false
    load()
  } catch (e: any) {
    ElMessage.error(e?.response?.data || '保存失败')
  } finally {
    submitting.value = false
  }
}

const toggleNode = async (row: SelfNode) => {
  await client.post(`/self-nodes/${row.id}/toggle`, { enabled: !row.enabled })
  ElMessage.success(row.enabled ? '已禁用' : '已启用')
  load()
}

const deleteNode = async (row: SelfNode) => {
  await ElMessageBox.confirm('确定删除此自建节点？', '确认', { type: 'warning' })
  await client.delete(`/self-nodes/${row.id}`)
  ElMessage.success('已删除')
  load()
}

const openImportDialog = () => {
  importUrl.value = ''
  importDialogVisible.value = true
}

const importNode = () => {
  const url = importUrl.value.trim()
  if (!url) {
    ElMessage.warning('请输入节点链接')
    return
  }

  try {
    const parsed = parseNodeUrl(url)
    if (!parsed) {
      ElMessage.error('无法解析节点链接，请检查格式')
      return
    }

    // 填充表单
    form.value = { ...emptyForm(), ...parsed }
    importDialogVisible.value = false
    // 延迟打开编辑对话框，避免同时关闭和打开
    setTimeout(() => {
      dialogVisible.value = true
      editMode.value = false
      editingId.value = null
    }, 100)
    ElMessage.success('节点已导入，请检查并保存')
  } catch (e: any) {
    ElMessage.error(e.message || '导入失败')
  }
}

// 解析节点 URL
const parseNodeUrl = (url: string): Partial<Omit<SelfNode, 'id'>> | null => {
  try {
    if (url.startsWith('vless://')) {
      return parseVlessUrl(url)
    } else if (url.startsWith('vmess://')) {
      return parseVmessUrl(url)
    } else if (url.startsWith('ss://')) {
      return parseSsUrl(url)
    } else if (url.startsWith('trojan://')) {
      return parseTrojanUrl(url)
    }
    return null
  } catch (e) {
    console.error('Parse error:', e)
    return null
  }
}

// 解析 vless:// URL
const parseVlessUrl = (url: string): Partial<Omit<SelfNode, 'id'>> => {
  // vless://uuid@server:port?encryption=none&security=tls&type=grpc&serviceName=xxx#name
  const match = url.match(/^vless:\/\/([^@]+)@([^:]+):(\d+)(\?[^#]*)?(#(.*))?$/)
  if (!match) throw new Error('Invalid vless URL')

  const [, uuid, server, portStr, query, , name] = match
  const port = parseInt(portStr)
  const params = new URLSearchParams(query || '')

  return {
    name: name ? decodeURIComponent(name) : `VLESS-${server}`,
    protocol: 'vless',
    server,
    port,
    uuid,
    network: params.get('type') || 'tcp',
    tls: params.get('security') === 'tls',
    grpc_service_name: params.get('serviceName') || '',
    enabled: true
  }
}

// 解析 vmess:// URL
const parseVmessUrl = (url: string): Partial<Omit<SelfNode, 'id'>> => {
  // vmess:// 后面是 base64 编码的 JSON
  const base64 = url.replace('vmess://', '')
  const json = JSON.parse(atob(base64))

  return {
    name: json.ps || `VMess-${json.add}`,
    protocol: 'vmess',
    server: json.add,
    port: parseInt(json.port),
    uuid: json.id,
    alter_id: parseInt(json.aid || '0'),
    cipher: json.scy || 'auto',
    network: json.net || 'tcp',
    tls: json.tls === 'tls',
    grpc_service_name: json.path || '',
    enabled: true
  }
}

// 解析 ss:// URL
const parseSsUrl = (url: string): Partial<Omit<SelfNode, 'id'>> => {
  // ss://base64(method:password)@server:port#name
  const match = url.match(/^ss:\/\/([^@]+)@([^:]+):(\d+)(#(.*))?$/)
  if (!match) throw new Error('Invalid ss URL')

  const [, encoded, server, portStr, , name] = match
  const decoded = atob(encoded)
  const [cipher, password] = decoded.split(':')

  return {
    name: name ? decodeURIComponent(name) : `SS-${server}`,
    protocol: 'ss',
    server,
    port: parseInt(portStr),
    cipher,
    password,
    enabled: true
  }
}

// 解析 trojan:// URL
const parseTrojanUrl = (url: string): Partial<Omit<SelfNode, 'id'>> => {
  // trojan://password@server:port#name
  const match = url.match(/^trojan:\/\/([^@]+)@([^:]+):(\d+)(#(.*))?$/)
  if (!match) throw new Error('Invalid trojan URL')

  const [, password, server, portStr, , name] = match

  return {
    name: name ? decodeURIComponent(name) : `Trojan-${server}`,
    protocol: 'trojan',
    server,
    port: parseInt(portStr),
    password,
    enabled: true
  }
}

onMounted(load)
</script>
