# ADR 0038: update 的 latest 解析接入双通道(契约反转)

## 状态

accepted

## 上下文

ADR 0037 决策 2 把"显式镜像 + latest 维持 fail closed(显式 = 用户接管,契约不变)"定为冻结契约,并把"update 的自动回退"留作后续。该后续在落地时发现契约本身值得反转:

1. ADR 0037 的初衷是防"latest 解析依赖 GitHub"卡住镜像模式;但 latest 解析的兜底通道(jsDelivr 数据 API)**与下载基无关**——它不从镜像取数据,而是从 jsDelivr 列 tag。因此"显式镜像 ⇒ 必须显式版本"的因果链在 jsDelivr 可达时并不成立,无论镜像是显式给的还是档案继承的。
2. update 侧旧实现(`api.github.com` 内联解析)与安装侧(重定向 + jsDelivr)已是两条平行链,维护双份语义无谓;`api_prerelease` 解析经核实为事实死代码(`/releases/latest` 按 GitHub 语义永不指向 prerelease)。
3. 用户体验目标(国内裸 `proxyhubctl update` 可用)与冻结契约直接冲突。

## 决策

1. **update 的 latest 解析切换到 `resolve_latest_version`**(GitHub 重定向 → jsDelivr 数据 API),与安装器同一条链;删除"显式镜像 + latest 必须显式版本"的 rc2 门禁——无论镜像来自档案还是显式参数。双通道都失败时照旧 fail closed(rc 1 + 显式版本指引)。
2. **prerelease 门禁语义不变但实现简化**:两条 latest 通道都只产稳定版(GitHub `/releases/latest` 语义 + jsDelivr 候选的 `*-*` 名形过滤 + `validate_version`),`api_prerelease` 死代码删除;显式版本的预发布门禁(`--prerelease` / `--stable-only`)继续由名形检查承担。
3. ADR 0037 决策 2 中"显式镜像维持 fail closed"一句由此**作废**;决策 1(默认路径自动回退)、3(显式参数优先)、4(隐私告知)不受影响。

## 理由

- 信任锚(minisign 验签)管完整性,latest 解析只管"选哪个版本"——两条通道给出同样的稳定版集合时,放开显式镜像的限制不引入任何安全语义变化。
- 单链双通道消除安装/更新两套 latest 语义漂移的空间。
- 契约反转显式记录,避免"ADR 说不变、代码已放开"的治理腐坏。

## 后果

- 裸 `proxyhubctl update` 在国内镜像模式下可用(jsDelivr 可达时);镜像模式下自动更新(auto-update timer)随之可用,其 latest 依赖 jsDelivr 可达性,DEPLOY.md 注意事项已按此改写。
- 显式 `--version` 仍是双通道全断时的逃生门,文案指引保留。
