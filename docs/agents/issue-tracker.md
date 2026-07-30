# Issue 跟踪器:GitHub

本仓库的 issue 与 PRD 以 GitHub issue 形式存在。一切操作使用 `gh` CLI。

## 约定

- **创建 issue**:`gh issue create --title "..." --body "..."`。多行正文用 heredoc。
- **读取 issue**:`gh issue view <number> --comments`,可用 `jq` 过滤评论,同时取标签。
- **列出 issue**:`gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'`,按需加 `--label` 与 `--state` 过滤。
- **评论**:`gh issue comment <number> --body "..."`
- **加/移标签**:`gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **关闭**:`gh issue close <number> --comment "..."`

仓库身份从 `git remote -v` 推断——在 clone 内运行时 `gh` 自动识别。

## Pull request 作为 triage 面

**PR 作为请求面:否。**(若本仓库把外部 PR 当作功能请求对待,改为 `yes`;`/triage` 读此开关。)

置为 `yes` 时,PR 走与 issue 相同的标签与状态,使用对应的 `gh pr` 命令:

- **读 PR**:`gh pr view <number> --comments`;diff 用 `gh pr diff <number>`。
- **列出待 triage 的外部 PR**:`gh pr list --state open --json number,title,body,labels,author,authorAssociation,comments`,只保留 `authorAssociation` 为 `CONTRIBUTOR`、`FIRST_TIME_CONTRIBUTOR` 或 `NONE` 的(丢弃 `OWNER`/`MEMBER`/`COLLABORATOR`)。
- **评论/标签/关闭**:`gh pr comment`、`gh pr edit --add-label`/`--remove-label`、`gh pr close`。

GitHub 的 issue 与 PR 共用一个编号空间,裸 `#42` 可能任一——先 `gh pr view 42` 解析,失败再 `gh issue view 42`。

## 当 skill 说"发布到 issue 跟踪器"时

创建一个 GitHub issue。

## 当 skill 说"取相关 ticket"时

运行 `gh issue view <number> --comments`。

## 寻路操作(wayfinding)

供 `/wayfinder` 使用。**map** 是单个 issue,**子 ticket** 挂在其下。

- **Map**:单个带 `wayfinder:map` 标签的 issue,正文承载 Notes / Decisions-so-far / Fog。`gh issue create --label wayfinder:map`。
- **子 ticket**:作为 GitHub 子 issue 关联到 map(子 issue 端点用 `gh api`)。子 issue 不可用时,把它加进 map 正文的任务列表,并在子 issue 正文顶部写 `Part of #<map>`。标签:`wayfinder:<type>`(`research`/`prototype`/`grilling`/`task`)。认领后把 ticket assign 给驱动的开发者。
- **阻塞**:GitHub **原生 issue 依赖**——规范的、UI 可见的表示。加边:`gh api --method POST repos/<owner>/<repo>/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>`,其中 `<blocker-db-id>` 是阻塞方的数字 **database id**(`gh api repos/<owner>/<repo>/issues/<n> --jq .id`,不是 `#number` 或 `node_id`)。GitHub 报告 `issue_dependencies_summary.blocked_by`(只计未关闭的阻塞方——这是实时闸门)。依赖不可用时退化为在子 issue 正文顶部写 `Blocked by: #<n>, #<n>`。所有阻塞方关闭后 ticket 解除阻塞。
- **Frontier 查询**:列出 map 的未关闭子 issue(`gh issue list --state open`,限定在 map 的子 issue/任务列表内),丢弃有未关闭阻塞方(`issue_dependencies_summary.blocked_by > 0`,或 `Blocked by` 行内有未关闭 issue)或已有 assignee 的;按 map 顺序取第一个。
- **认领**:`gh issue edit <n> --add-assignee @me`——会话的第一次写操作。
- **了结**:`gh issue comment <n> --body "<answer>"`,然后 `gh issue close <n>`,再把上下文指针(gist + 链接)追加到 map 的 Decisions-so-far。
