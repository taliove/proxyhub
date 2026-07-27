import { ref } from 'vue'

import { listTemplates, type Template } from '@/api/templates'

// useTemplateList 模板库列表的共享加载逻辑(订阅地址创建对话框、详情抽屉、
// 其他只需要"列一遍库"的消费方共用)。模板库管理页(TemplateEditor)有选中/
// 编辑联动,不在此列。
export function useTemplateList() {
  const templates = ref<Template[]>([])
  const loading = ref(false)

  async function loadTemplates() {
    loading.value = true
    try {
      const data = await listTemplates()
      templates.value = data.templates
    } catch {
      // 全局拦截器已 toast;库为空/加载失败时下拉只留"跟随默认模板"
      templates.value = []
    } finally {
      loading.value = false
    }
  }

  return { templates, loading, loadTemplates }
}
