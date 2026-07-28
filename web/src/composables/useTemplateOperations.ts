// Template CRUD operations composable
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  listTemplates,
  getTemplate,
  createTemplate,
  deleteTemplate,
  setDefaultTemplate,
  resetTemplate,
  type Template
} from '@/api/templates'
import { extractErrorDetail } from '@/utils/errors'
import client from '@/api/client'

export function useTemplateOperations() {
  const templates = ref<Template[]>([])
  const loading = ref(true)
  const errorMsg = ref('')

  // Load all templates
  async function loadTemplates(): Promise<Template[]> {
    loading.value = true
    errorMsg.value = ''
    try {
      const data = await listTemplates()
      templates.value = data.templates
      return data.templates
    } catch (e) {
      const detail = extractErrorDetail(e)
      errorMsg.value = detail || '加载模板列表失败'
      return []
    } finally {
      loading.value = false
    }
  }

  // Create a new template
  async function create(
    name: string,
    existingTemplates: Template[],
    onSuccess: (newTemplate: Template) => void
  ): Promise<boolean> {
    if (!name.trim()) {
      ElMessage.error('模板名称不能为空')
      return false
    }

    try {
      // 新模板以当前生效模板为底稿:库默认成员 ?? 回退链生效值(全局默认 ?? 内嵌)。
      let scaffold = ''
      const defaultMember = existingTemplates.find((t) => t.is_default) ?? existingTemplates[0]
      if (defaultMember) {
        scaffold = (await getTemplate(defaultMember.name)).content ?? ''
      } else {
        const resp = await client.get<unknown, { template: string }>('/settings/template', {
          skipErrorToast: true
        })
        scaffold = resp.template
      }

      const newTmpl = await createTemplate({ name, content: scaffold })
      ElMessage.success('模板创建成功')
      onSuccess(newTmpl)
      return true
    } catch (e) {
      const detail = extractErrorDetail(e)
      if (detail?.includes('quota exceeded')) {
        ElMessage.error('模板数量已达配额上限，请删除不需要的模板后重试')
      } else if (detail?.includes('already exists')) {
        ElMessage.error('模板名称已存在')
      } else {
        ElMessage.error(detail || '创建模板失败')
      }
      return false
    }
  }

  // Delete a template
  async function remove(tmpl: Template, refCount: number): Promise<boolean> {
    const refWarning = refCount > 0 ? `,${refCount} 个订阅地址将改用默认模板` : ''

    try {
      await ElMessageBox.confirm(
        `确定删除模板「${tmpl.name}」吗${refWarning}?此操作无法撤销。`,
        '删除模板',
        {
          type: 'warning',
          confirmButtonText: '删除',
          cancelButtonText: '取消'
        }
      )
    } catch {
      return false
    }

    errorMsg.value = ''
    try {
      const result = await deleteTemplate(tmpl.name)
      if (result.ref_count > 0) {
        ElMessage.success(
          `已删除模板「${tmpl.name}」，${result.ref_count} 个订阅地址将改用默认模板`
        )
      } else {
        ElMessage.success(`已删除模板「${tmpl.name}」`)
      }
      return true
    } catch (e) {
      const detail = extractErrorDetail(e)
      errorMsg.value = detail || '删除失败'
      return false
    }
  }

  // Set a template as default
  async function setDefault(tmpl: Template): Promise<boolean> {
    if (tmpl.is_default) return false

    errorMsg.value = ''
    try {
      await setDefaultTemplate(tmpl.name)
      ElMessage.success(`已将「${tmpl.name}」设为默认模板`)
      return true
    } catch (e) {
      const detail = extractErrorDetail(e)
      errorMsg.value = detail || '设置默认失败'
      return false
    }
  }

  // Reset a template to embedded default
  async function reset(name: string): Promise<boolean> {
    errorMsg.value = ''
    try {
      await resetTemplate(name)
      ElMessage.success(`已将「${name}」重设为默认模板`)
      return true
    } catch (e) {
      const detail = extractErrorDetail(e)
      errorMsg.value = detail || '重设默认失败'
      return false
    }
  }

  return {
    templates,
    loading,
    errorMsg,
    loadTemplates,
    create,
    remove,
    setDefault,
    reset
  }
}
