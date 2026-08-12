import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

// useTabQuery 受控 tab 与 ?tab= 双向同步(Settings 深链,快修批⑥)。
// deniedTabs(角色不可见的 tab 名)与空 query 都落到 fallback。
export function useTabQuery(fallback: () => string, deniedTabs: string[] = []) {
  const route = useRoute()
  const router = useRouter()

  const resolve = (q: unknown): string => {
    const t = typeof q === 'string' ? q : ''
    return t && !deniedTabs.includes(t) ? t : fallback()
  }

  const activeTab = ref(resolve(route.query.tab))

  watch(
    () => route.query.tab,
    (q) => {
      const t = resolve(q)
      if (t !== activeTab.value) activeTab.value = t
    }
  )
  watch(activeTab, (t) => {
    if (t && route.query.tab !== t) router.replace({ query: { ...route.query, tab: t } })
  })

  return { activeTab }
}
