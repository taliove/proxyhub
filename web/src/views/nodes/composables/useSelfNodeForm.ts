import { ref, type Ref } from 'vue'
import type { SelfNode } from '@/types'
import type { UnifiedNode } from '../selfmerge'
import { emptyForm, type SelfNodeForm } from '../self-node-utils'

// Self-node form / import dialog state machine, extracted from the nodes view assembly
// (400-line gate). Owns dialog visibility, edit mode and form payload; persistence stays
// injected (saveSelf / reloadPool) so the view keeps owning data sources.
export function useSelfNodeForm(opts: {
  selfIndex: Ref<Map<number, SelfNode>>
  saveSelf: (form: SelfNodeForm, editingId: number | null) => Promise<boolean>
  reloadPool: () => Promise<void> | void
}) {
  const { selfIndex, saveSelf, reloadPool } = opts

  const selfDialogVisible = ref(false)
  const selfEditMode = ref(false)
  const selfEditingId = ref<number | null>(null)
  const selfForm = ref<SelfNodeForm>(emptyForm())
  const selfSubmitting = ref(false)
  const importDialogVisible = ref(false)

  const openAddSelf = () => {
    selfEditMode.value = false
    selfEditingId.value = null
    selfForm.value = emptyForm()
    selfDialogVisible.value = true
  }
  const openEditSelf = (row: UnifiedNode) => {
    if (row.self_node_id == null) return
    const sn = selfIndex.value.get(row.self_node_id)
    if (!sn) return
    const { name, protocol, server, port, uuid, password, cipher } = sn
    const { alter_id, network, tls, grpc_service_name, grpc_authority, enabled } = sn
    selfEditMode.value = true
    selfEditingId.value = sn.id
    selfForm.value = {
      name,
      protocol,
      server,
      port,
      uuid,
      password,
      cipher,
      alter_id,
      network,
      tls,
      grpc_service_name,
      grpc_authority,
      enabled
    }
    selfDialogVisible.value = true
  }
  const submitSelfForm = async () => {
    if (selfSubmitting.value) return
    selfSubmitting.value = true
    try {
      const ok = await saveSelf(selfForm.value, selfEditMode.value ? selfEditingId.value : null)
      if (ok) {
        selfDialogVisible.value = false
        await reloadPool()
      }
    } finally {
      selfSubmitting.value = false
    }
  }
  const openImport = () => {
    importDialogVisible.value = true
  }
  // Import result fills the form; after the import dialog closes, open the editor in create
  // mode (delayed to avoid both dialogs toggling at once).
  const onImported = (parsed: Partial<SelfNodeForm>) => {
    selfForm.value = { ...emptyForm(), ...parsed }
    importDialogVisible.value = false
    selfEditMode.value = false
    selfEditingId.value = null
    setTimeout(() => {
      selfDialogVisible.value = true
    }, 100)
  }

  return {
    selfDialogVisible,
    selfEditMode,
    selfForm,
    selfSubmitting,
    importDialogVisible,
    openAddSelf,
    openEditSelf,
    submitSelfForm,
    openImport,
    onImported
  }
}
