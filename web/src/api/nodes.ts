import client from './client'

// 节点相关 API(issue #83 起;节点列表本身仍由 useNodePool 直取,见该文件注释)。

// setNodeFavorite 设置/取消节点收藏:POST /api/nodes/{nodeKey}/favorite。
// 收藏是展示层星标,服务端持久(node_overrides.favorite),跨刷新/跨设备可见。
export const setNodeFavorite = (nodeKey: string, favorite: boolean) =>
  client.post<unknown, { success: boolean }>(`/nodes/${encodeURIComponent(nodeKey)}/favorite`, {
    favorite
  })
