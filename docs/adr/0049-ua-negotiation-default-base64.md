# ADR 0049: UA 分流默认方向翻转——Clash 系 UA 得 YAML,其余默认通用 base64

## 状态

accepted

## 日期

2026-08-14

## 上下文

订阅地址未携带显式 `format` 参数时按请求方 User-Agent 判定订阅格式(术语见 CONTEXT.md「UA 分流」「订阅格式」)。翻转前的判定是:UA 含 v2ray/shadowrocket 时发通用 base64,其余一切(含空 UA、浏览器、curl、未知客户端)默认发 Clash YAML。

这个默认方向与安全姿态相悖:Clash YAML 由模板引擎渲染,内含模板骨架、规则集、策略组与面板指纹,是信息最全的格式;认不出的客户端反而拿到最大暴露面(反例参考:TAG 系面板对未知 UA 直接 400/403 封禁)。同时 sing-box 等白名单外客户端拿到 YAML 后无法直接导入,体验也错。

显式 `format` 参数(`clash`/`base64`,`v2ray` 为 base64 的永久别名,issue #121)永远优先于 UA 判定,不在本决策范围内。

## 决策

1. **默认方向翻转**:命中 Clash 系 UA 名单的请求得 Clash YAML;其余一切请求(空 UA、浏览器、curl、未知客户端)默认得通用 base64 订阅。认不出的一律给信息最少的格式——最小必要暴露。
2. **名单为可维护常量表**(`internal/server/subscription_format.go` 的 `clashUATokens`):子串匹配、不区分大小写,初值 `clash` / `mihomo` / `stash`(`clash` 天然覆盖 clash.meta / clash-verge / flclash / clashx 等 clash 前缀变体)。新增 Clash 系客户端在此追加。
3. **只做格式分流,不做客户端封禁**:不学 TAG 式 400/403;任何请求方都能拿到可用订阅,只是信息面不同。
4. **被放弃的旧默认**「未知 UA 发 YAML」:它把最大信息面给了最不可识别的请求方,且对非 Clash 客户端(浏览器扫码外的直接导入、curl 调试、sing-box)产出不可消费的内容。翻转的代价是"未知 UA 的 Clash 用户"需要显式 `format=clash` 或一键导入入口(issue #123)——可发现、可纠正,故接受。
5. **回归红线不变**:Shadowrocket UA 的 REMARKS 明文行注入(Shadowrocket 走 base64,注入条件不变)、状态虚拟节点双格式渲染、既有响应头(Profile-Title / Content-Disposition / Profile-Update-Interval / Cache-Control: no-store)、模板四级回退链全部不变;UA 误判面(大小写、子串边界、空 UA)由测试钉死。

## 后果

- 非 Clash 请求方默认只拿到节点连接串集合,模板/规则/面板指纹不再下发——收窄了被动指纹面。
- sing-box、Shadowrocket、v2rayNG 等通用客户端无需显式参数即可直接导入。
- Clash 系客户端 UA 名单是新的维护点:漏名单的 Clash 客户端会拿到 base64(不可用),靠常量表追加与 `format=clash` 显式参数兜底。
- 与 ADR 0044(入站方向的内容嗅探)方向相反、互不冲突:入站宽容(什么都能解析),出站保守(认不出就给最少)。

## 相关决策

- ADR 0044(Clash YAML 机场订阅的内容嗅探解析):入站方向的宽容解析,与本 ADR 出站方向的保守下发构成对照。
- issue #121(format=base64 规范值 + v2ray 永久别名):本决策依赖其规范化边界,显式参数语义不变。
