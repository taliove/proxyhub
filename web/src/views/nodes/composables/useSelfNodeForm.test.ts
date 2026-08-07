import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref } from 'vue'
import { useSelfNodeForm } from './useSelfNodeForm'
import { emptyForm, type SelfNodeForm } from '../self-node-utils'
import type { SelfNode } from '@/types'
import type { UnifiedNode } from '../selfmerge'

const selfNode = (over: Partial<SelfNode> = {}): SelfNode =>
  ({
    id: 7,
    name: 'self-a',
    protocol: 'vmess',
    server: 'example.com',
    port: 443,
    uuid: '00000000-0000-0000-0000-000000000000',
    password: '',
    cipher: 'auto',
    alter_id: 0,
    network: 'tcp',
    tls: true,
    grpc_service_name: '',
    enabled: true,
    ...over
  }) as SelfNode

const setup = (nodes: SelfNode[] = [selfNode()]) => {
  const selfIndex = ref(new Map(nodes.map((n) => [n.id, n])))
  const saveSelf = vi.fn<(form: SelfNodeForm, id: number | null) => Promise<boolean>>()
  const reloadPool = vi.fn()
  const form = useSelfNodeForm({ selfIndex, saveSelf, reloadPool })
  return { selfIndex, saveSelf, reloadPool, form }
}

describe('useSelfNodeForm', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('openAddSelf:重置为新建态并打开对话框', () => {
    const { form } = setup()
    form.openEditSelf({ self_node_id: 7 } as UnifiedNode)
    form.openAddSelf()
    expect(form.selfEditMode.value).toBe(false)
    expect(form.selfForm.value).toEqual(emptyForm())
    expect(form.selfDialogVisible.value).toBe(true)
  })

  it('openEditSelf:按 self_node_id 从索引预填完整配置', () => {
    const { form } = setup()
    form.openEditSelf({ self_node_id: 7 } as UnifiedNode)
    expect(form.selfEditMode.value).toBe(true)
    expect(form.selfForm.value.name).toBe('self-a')
    expect(form.selfForm.value.server).toBe('example.com')
    expect(form.selfForm.value.uuid).toBe('00000000-0000-0000-0000-000000000000')
    expect(form.selfDialogVisible.value).toBe(true)
  })

  it('openEditSelf:非自建行（无 self_node_id）不打开对话框', () => {
    const { form } = setup()
    form.openEditSelf({ self_node_id: null } as unknown as UnifiedNode)
    expect(form.selfDialogVisible.value).toBe(false)
  })

  it('submitSelfForm:保存成功关闭对话框并刷新池；新建传 null id', async () => {
    const { form, saveSelf, reloadPool } = setup()
    saveSelf.mockResolvedValue(true)
    form.openAddSelf()
    await form.submitSelfForm()
    expect(saveSelf).toHaveBeenCalledWith(emptyForm(), null)
    expect(form.selfDialogVisible.value).toBe(false)
    expect(reloadPool).toHaveBeenCalled()
    expect(form.selfSubmitting.value).toBe(false)
  })

  it('submitSelfForm:编辑态传编辑 id;保存失败保持对话框', async () => {
    const { form, saveSelf } = setup()
    saveSelf.mockResolvedValue(false)
    form.openEditSelf({ self_node_id: 7 } as UnifiedNode)
    await form.submitSelfForm()
    expect(saveSelf.mock.calls[0][1]).toBe(7)
    expect(form.selfDialogVisible.value).toBe(true)
  })

  it('onImported:导入结果填充表单，延迟以新建模式打开编辑框', () => {
    const { form } = setup()
    form.openImport()
    form.onImported({ name: 'imported', server: 'example.com' })
    expect(form.importDialogVisible.value).toBe(false)
    expect(form.selfEditMode.value).toBe(false)
    expect(form.selfForm.value.name).toBe('imported')
    expect(form.selfForm.value.protocol).toBe('ss') // emptyForm 默认补齐
    expect(form.selfDialogVisible.value).toBe(false)
    vi.advanceTimersByTime(100)
    expect(form.selfDialogVisible.value).toBe(true)
  })
})
