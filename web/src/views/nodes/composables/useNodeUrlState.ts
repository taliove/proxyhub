import { watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { NodeFilterCriteria } from '../predicates'
import type { ScoreLevel } from '@/components/exam/stability'

// useNodeUrlState 将节点筛选条件与详情抽屉状态同步到 URL query,支持分享与刷新。
// 筛选条件的简单值(字符串/布尔)直接映射到 query;数组(tags/unlock)用逗号分隔。
// 详情抽屉用 ?detail=<node_key> 标识;打开抽屉时推入历史,关闭时回退。
export function useNodeUrlState(
  criteria: NodeFilterCriteria,
  detailVisible: Ref<boolean>,
  detailNodeKey: Ref<string | null>
) {
  const route = useRoute()
  const router = useRouter()

  // 从 URL 恢复筛选条件(onMounted 前调用,替代原来的 route.query.source 处理)
  const restoreFromUrl = () => {
    const q = route.query
    if (typeof q.source === 'string') criteria.source = q.source
    if (typeof q.region === 'string') criteria.region = q.region
    if (typeof q.keyword === 'string') criteria.keyword = q.keyword
    if (typeof q.type === 'string') criteria.type = q.type
    if (q.available === 'true') criteria.available = true
    else if (q.available === 'false') criteria.available = false
    if (q.blocked === 'true') criteria.blocked = true
    else if (q.blocked === 'false') criteria.blocked = false
    if (q.stale === 'true') criteria.stale = true
    else if (q.stale === 'false') criteria.stale = false
    // 收藏筛选(issue #83):?favorite=true 直达"已收藏"视图
    if (q.favorite === 'true') criteria.favorite = true
    else if (q.favorite === 'false') criteria.favorite = false
    if (typeof q.tags === 'string' && q.tags) criteria.tags = q.tags.split(',')
    if (typeof q.unlock === 'string' && q.unlock) criteria.unlock = q.unlock.split(',')
    if (typeof q.stabilityBand === 'string' && isScoreLevel(q.stabilityBand)) {
      criteria.stabilityBand = q.stabilityBand
    }

    // 详情抽屉:如果 URL 带 detail=<node_key>,标记待打开(由父组件在数据加载后触发)
    if (typeof q.detail === 'string' && q.detail) {
      detailNodeKey.value = q.detail
    }
  }

  // 筛选条件变化时同步到 URL(replace 不增加历史)
  watch(
    () => ({ ...criteria }),
    () => {
      const query: Record<string, string | undefined> = {}
      if (criteria.source) query.source = criteria.source
      if (criteria.region) query.region = criteria.region
      if (criteria.keyword) query.keyword = criteria.keyword
      if (criteria.type) query.type = criteria.type
      if (criteria.available !== null) query.available = String(criteria.available)
      if (criteria.blocked !== null) query.blocked = String(criteria.blocked)
      if (criteria.stale !== null) query.stale = String(criteria.stale)
      if (criteria.favorite !== null) query.favorite = String(criteria.favorite)
      if (criteria.tags.length > 0) query.tags = criteria.tags.join(',')
      if (criteria.unlock.length > 0) query.unlock = criteria.unlock.join(',')
      if (criteria.stabilityBand) query.stabilityBand = criteria.stabilityBand

      // 保留 detail 参数(如果存在)
      if (route.query.detail) query.detail = String(route.query.detail)

      router.replace({ query })
    },
    { deep: true }
  )

  // 详情抽屉打开/关闭时同步 URL
  watch([detailVisible, detailNodeKey], ([visible, key]) => {
    const currentDetail = route.query.detail
    if (visible && key) {
      // 打开抽屉:push 增加历史(用户可后退关闭)
      if (currentDetail !== key) {
        router.push({ query: { ...route.query, detail: key } })
      }
    } else if (!visible && currentDetail) {
      // 关闭抽屉:移除 detail 参数
      const { detail, ...rest } = route.query
      router.replace({ query: rest })
    }
  })

  return { restoreFromUrl }
}

// 类型守卫:检查字符串是否为有效的 ScoreLevel
function isScoreLevel(s: string): s is ScoreLevel {
  return s === 'good' || s === 'fair' || s === 'poor'
}
