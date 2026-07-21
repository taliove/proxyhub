# ADR 0021: 节点地区真相链 - 出网优先于 GeoIP

状态: accepted
日期: 2026-07-21

## 上下文

节点地区码有三个可能来源,可信度不同:

1. **体检出网(egress)国家码**:经节点真实出口打 ip-api,拿到的是**真实出口国家**——地面真相。
2. **离线 GeoIP 反查**:拿节点 server 的 IP/域名查 DB-IP 库,是**基于入口地址的猜测**。中转/隧道/机房出口常与入口不在同一国,GeoIP 会猜错。
3. **节点名识别**:从名字里的"香港/HK/🇭🇰"提取,靠命名规范。

实测发现 GeoIP 猜测与真实出口不符(如入口在美国机房、出口落地香港)。早期回写策略是"仅当 region 为空才写",导致一个被 GeoIP 猜错的非空 region 永远得不到纠正。

## 决策

确立地区真相链:**出网 egress > 离线 GeoIP > 保留现值**,并据此改两处逻辑。

### 1. 体检回写:egress 总是覆盖

体检自然完成后(`writebackRegionIfNeeded`,`internal/server/server.go`),只要出网段拿到 IPv4 国家码,就**总是覆盖**节点 region——包括覆盖 GeoIP 猜错的非空旧值,不再"仅空才写"。无 egress 数据则不动。回写作用于机场节点(内存池)与自建节点(`self_hosted_nodes` 表 + 内存池同步),best-effort:失败只记日志,不阻断体检收口。

### 2. 地区解析单一事实源:优先序链

自建节点的地区解析统一走 `resolveNodeRegion`(`internal/server/region_resolution.go`),按优先序:

1. **最近体检 egress 国家码**(`LatestExamHistory` 的 `Egress.IPv4.CountryCode`)——真实出口。
2. **离线 GeoIP**(`resolveRegionGeoOnly`,拿 server 反查)——兜底猜测。
3. **保留现值**——两者都拿不到时不动。

"刷新名称"对自建节点即走此链:不再只靠 GeoIP,体检过的节点用真实出口纠正;重命名按修正后的 region 执行(`自建{地区中文名}`)。

## 后果

### 正面
- 被 GeoIP 猜错的地区能被真实体检纠正,节点国旗/分组不再长期错误。
- 地区解析逻辑收敛到 `resolveNodeRegion` 一处,体检回写与刷新名称共用,不再各写一套优先序。

### 负面
- egress"总是覆盖"意味着一次异常的出口(如节点临时改路由)也会立刻改写 region,直到下次体检再纠正;取"最近一次真实出口即真相",不做多次投票。
- 地区正确性依赖用户跑过体检:没体检过的节点仍只有 GeoIP 猜测。

### 被放弃的备选
- **仅当 region 为空才回写**:无法纠正 GeoIP 猜错的非空值,正是本 ADR 要解决的病根,否决。
- **GeoIP 优先、egress 兜底**:方向反了,把猜测置于真相之上,否决。

## 参考
- [ADR 0018](0018-nodekey-upsert-and-stale-lifecycle.md):机场节点 region 每轮刷新重算的 carry-forward 边界。
- [design-node-exam](../design-node-exam.md):出网段的 egress 数据结构。
