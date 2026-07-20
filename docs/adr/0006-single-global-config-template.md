# ADR 0006: 配置模板采用单一全局模板

## 状态

已接受

## 上下文

订阅地址生成的 Clash 配置原本只有 2 个策略组 + 1 条 MATCH 规则,无法满足实际使用(缺少应用分流、DNS 优化、hosts 等)。需要引入完整的 Clash 配置(hosts/dns/proxy-groups/rules),并支持后台编辑。

一个设计岔口:配置模板是**每个订阅地址一份**,还是**全局共享一份**?

## 决策

采用**单一全局模板**:所有订阅地址共享同一套 hosts/dns/proxy-groups/rules,生成时各自注入当前节点池。模板存 SQLite 的 `settings` 表(key=`clash_template`);默认模板从专业机场配置提取后内嵌进二进制(`internal/generator/default_template.yaml`),可一键恢复。

## 理由

- 用户创建多个订阅地址的动机是"给不同设备分发",不是"给不同设备用不同规则"(README 场景:iPhone/iPad/Mac 各一个订阅)
- 维护一份模板远比维护 N 份简单——数百条分流规则改一次即处处生效
- 若未来真出现"不同订阅要不同规则",可加一个可选的 `template_id` 字段扩展,不影响现有语义

## 后果

- 订阅生成从"硬编码生成"改为"读模板 → 渲染"。`renderSubscription` 成为 `*Server` 方法(需访问 store 读模板)。
- 改模板即时对下一次订阅生效,无需刷新——与 ADR 0004/0005 的即时生效语义一致。
- 旧的 `generator.GenerateClash` 被 `RenderTemplate` 取代并删除;`clashProxy`/`uniqueName` 保留复用。
- V2Ray 格式不走模板(它只是 base64 链接列表,无配置骨架可言)。

## 相关决策

- ADR 0005: 关键词过滤在订阅生成时生效
- ADR 0007: 占位符语法使用 Mustache 风格 `{{nodes}}`
