# ADR 0029: 本机实测后端代理实现

## Status

Accepted

## Context

"本机实测"功能的初始实现存在根本性错误:浏览器直接 `fetch` ProxyHub 自建端点(`/api/speedtest/download` 等),测量的是浏览器到 ProxyHub 服务器的本地回环速度(14000+ Mbps),完全没有使用用户选择的节点,导致:

1. 测速结果与节点实际性能无关
2. 数值虚高,误导用户
3. "选择节点"成为摆设

真实需求是:测量"用户浏览器 ↔ ProxyHub ↔ 节点 ↔ 公共测速端点"全链路的实际带宽,反映用户通过该节点的真实使用体验。

## Decision

### 架构变更

**原实现(错误)**:
```
浏览器 --fetch--> /api/speedtest/download --> ProxyHub 自建端点发流
        <--流式读--
```
测的是浏览器 ↔ ProxyHub 的本地链路。

**新实现(正确)**:
```
浏览器 --调用--> POST /api/speedtest/proxy-test
                    ↓
                ProxyHub 后端:
                  1. 解析 node_key/self_node_id
                  2. 建立到节点的代理连接(复用 internal/proxy)
                  3. 通过代理访问 Cloudflare speed test
                  4. 测量下行/上行带宽
                    ↓
浏览器 <--结果-- {down_mbps, up_mbps, ...}
```

### API 契约

**新端点**: `POST /api/speedtest/proxy-test`

**请求**:
```json
{
  "node_key": "1.2.3.4:443",        // 二选一
  "self_node_id": 123,               // 自建节点
  "mode": "download"                 // "download" | "upload" | "full"
}
```

**响应**:
```json
{
  "down_mbps": 245.8,
  "up_mbps": 89.3,
  "idle_latency_ms": 28.5,
  "jitter_ms": 3.2,
  "elapsed_ms": 12500
}
```

### 测速目标端点

复用"快速测速"(检查动作 3)的 Cloudflare speed test 端点:
- 下行: `https://speed.cloudflare.com/__down?bytes=100000000` (100MB)
- 上行: `https://speed.cloudflare.com/__up` (POST 随机数据)
- 延迟: `https://speed.cloudflare.com/__down?bytes=1000` (1KB 小请求,测 8 次)

### 前端改造

**移除**: `web/src/views/speedtest/runner.ts` 中的浏览器直接 fetch 逻辑(`measureDownload`/`measureUpload`/`measureLatency`)

**新增**: 调用 `POST /api/speedtest/proxy-test`,轮询或 SSE 获取进度(如需实时大数字)

**保持不变**:
- UI 组件(`ResultCards`、`HistoryTable`)
- 结果存储(`saveSpeedtestResult`)
- 历史记录查询

### 直连模式

请求体不传 `node_key`/`self_node_id`,或传 `node_key: ""`(空串),后端直接访问测速端点,不建立代理,作为基线对比。

## Consequences

### Positive

1. **语义正确**: 真正测量用户通过节点的实际带宽
2. **数值可信**: 不再是虚高的本地回环速度
3. **可对比**: 经节点 vs 直连的差值反映节点开销
4. **复用基建**: 后端代理逻辑复用 `internal/proxy`,测速端点复用检查动作

### Negative

1. **受 ProxyHub 带宽约束**: 测得速度上限 = min(ProxyHub 带宽, 节点带宽, 用户带宽),但这反映真实使用场景
2. **后端负载**: 测速流量经过 ProxyHub,但单次测速 10-20 秒可接受
3. **无法测"用户客户端 → 节点"**: 如果用户在客户端选了节点,ProxyHub 无法感知,需用户手动标注(但这是本机实测的既定语义)

## Implementation Notes

1. 后端需处理节点连接失败(超时/不可达)
2. 下行/上行测速时长可配置(默认各 10 秒)
3. 支持取消(abort signal)
4. 错误信息需区分:节点连接失败 vs 测速端点不可达 vs 网络超时

## References

- `CONTEXT.md`: 本机实测定义(已更新)
- `internal/proxy`: 节点代理连接实现
- `web/src/views/speedtest/`: 前端实测页面
