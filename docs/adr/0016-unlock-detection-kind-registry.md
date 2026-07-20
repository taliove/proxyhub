# ADR 0016: 解锁判定的 kind 注册表与保守判定原则

## 状态
**已采纳** — 2026-07

## 背景

流媒体/AI 解锁判定不是"HTTP 200 就算通"那么简单:

- Netflix 要区分"全解锁 / 仅自制剧 / 未解锁"(自制剧全球可放,非自制片受版权地区限制)。
- OpenAI / Claude / Gemini 是二元可用性,但要顺带解析出口国家码。
- 每个平台的判定逻辑(请求哪个 URL、看响应里哪个标记、地区从哪解析)各不相同,且会随平台策略变化。

若用一个大 `switch(target.name)` 塞所有平台逻辑,新增平台要改核心分发函数,且通用探测(generic)与专用判定混在一处,容易互相污染。同时,判定拿不准时如何取值(算解锁还是不解锁?地区猜还是留空?)需要一条统一原则,否则各平台各行其是,结果不可信。

## 决策

### 1. kind 注册表,而非 switch

`detection.Target` 加 `Kind` 字段(`internal/detection/kind.go`):

- `""` / `generic` 走现有通用探测逻辑;`netflix` / `youtube_premium` / `disney_plus` / `openai` / `claude` / `gemini` 六种专用 kind 各有判定器。
- 专用判定器经 `RegisterUnlockChecker(kind, checker)` 注册进 `unlockCheckers map[Kind]UnlockChecker`,每平台一个文件(`unlock_netflix.go` 等)自注册。新增平台 = 加一个文件 + 注册,不改分发核心。
- `resolveKind` 校验:未知 kind **报错,绝不静默按 generic 处理**;未注册的已知 kind 返回"未实现"错误。重复注册/给非法 kind 注册直接 panic(编程错误,启动即暴露)。
- 首次启动播种六个默认 Target(`DefaultUnlockTargets`),用户可编辑/删除/追加 generic Target。

### 2. 保守判定原则(拿不准就降级,不夸大)

- 流媒体三档 `full` / `originals_only` / `blocked`:只有明确能看非自制内容才判 `full`;仅自制可看判 `originals_only`;两者都不确定判 `blocked`。宁可少报解锁,不可虚报。
- AI 二元判定:命中"地区不支持"标记(如 403 + 特定文案)才判不可用;否则按可用。
- 出口地区**尽力解析**:顺手从响应或 `cloudflare.com/cdn-cgi/trace` 的 `loc=` 取国家码;解析不到就**留空**,不猜测、不回填默认值。

## 后果

### 正面
- 平台判定相互隔离,新增/调整某平台不波及其他,也不碰 generic 逻辑。
- 未知 kind 立刻报错而非静默降级,配置错误无处藏身。
- 结果宁缺毋滥:UI 上"解锁"是可信的强信号,空地区如实反映"没测出来"。

### 负面
- 平台改版会让专用判定失真(硬编码的片单 ID / 标记文案),需要跟进维护。
- 保守取值下,边缘可解锁的情况可能被判 `blocked`,偏悲观。

## 参考
- [ADR 0015](0015-node-exam-capability-boundary.md):体检能力边界(解锁归批量检测)。
- [design-node-exam](../design-node-exam.md):level/region 字段语义与三段模型。
