---
name: ProxyHub
description: 多机场订阅聚合系统的管理后台设计系统——电波塔台,冷静的仪器。
colors:
  primary: "#0c8078"
  primary-hover: "#0faea2"
  primary-active: "#0a6963"
  success: "#059669"
  warning: "#d97706"
  danger: "#dc2626"
  info: "#45565c"
  page-bg: "#f7fafb"
  surface: "#ffffff"
  hover-bg: "#eff4f5"
  border: "#e0e8ea"
  border-light: "#eff4f5"
  text-primary: "#0f1b20"
  text-regular: "#324249"
  text-secondary: "#617379"
  text-placeholder: "#91a3a8"
typography:
  display:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"
    fontSize: "32px"
    fontWeight: 700
    lineHeight: 1
  display-sm:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"
    fontSize: "28px"
    fontWeight: 700
    lineHeight: 1
  headline:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"
    fontSize: "20px"
    fontWeight: 600
    lineHeight: 1.3
  title:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"
    fontSize: "16px"
    fontWeight: 600
    lineHeight: 1.4
  body:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
  label:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"
    fontSize: "13px"
    fontWeight: 400
    lineHeight: 1.5
  label-xs:
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif"
    fontSize: "12px"
    fontWeight: 400
    lineHeight: 1.5
  mono:
    fontFamily: "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace"
    fontSize: "14px"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  sm: "4px"
  md: "6px"
  lg: "8px"
  xl: "12px"
  full: "999px"
spacing:
  "1": "4px"
  "2": "8px"
  "3": "12px"
  "4": "16px"
  "5": "24px"
  "6": "32px"
  "7": "48px"
  "8": "64px"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "#ffffff"
    rounded: "{rounded.sm}"
    padding: "5px 11px"
    height: "24px"
  button-primary-hover:
    backgroundColor: "{colors.primary-hover}"
    textColor: "#ffffff"
    rounded: "{rounded.sm}"
    padding: "5px 11px"
    height: "24px"
  button-primary-active:
    backgroundColor: "{colors.primary-active}"
    textColor: "#ffffff"
    rounded: "{rounded.sm}"
    padding: "5px 11px"
    height: "24px"
  button-default:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-regular}"
    rounded: "{rounded.sm}"
    padding: "5px 11px"
    height: "24px"
  button-danger:
    backgroundColor: "{colors.danger}"
    textColor: "#ffffff"
    rounded: "{rounded.sm}"
    padding: "5px 11px"
    height: "24px"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.sm}"
    padding: "4px 11px"
    height: "24px"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.text-primary}"
    rounded: "{rounded.lg}"
    padding: "16px"
  tag:
    backgroundColor: "{colors.hover-bg}"
    textColor: "{colors.text-regular}"
    rounded: "{rounded.full}"
    padding: "0 8px"
    height: "20px"
  status-dot:
    backgroundColor: "{colors.success}"
    size: "8px"
    rounded: "{rounded.full}"
---

# Design System: ProxyHub

## Overview

**Creative North Star: "电波塔台" (The Signal Tower)**

运营者坐在塔台里:上游是若干"机场",进出港的航班是节点,雷达屏是健康检查与任务中心。ProxyHub 的界面就是这座塔台的操作台——所有信息平铺在仪器面板上,调度动作一键可达,任何异常在影响乘客(终端设备)之前先亮在屏幕上。

气质是一台**冷静的仪器**:90% 的中性灰阶构成面板本体,电波青只在主行动、激活态与焦点环上出现,像仪器上唯一的那颗蓝色按键。信息密度优先于留白,控件默认 small 尺寸,视觉层级靠字重、字号与灰阶深浅表达,不靠色块堆砌。暗色是一等公民:不是反色滤镜,表面、文本、描边、阴影均有独立暗色取值。

已确认的视觉反参照:Element Plus 默认主题(#409eff 默认蓝、大圆角、厚阴影的"模板感"),以及一切装饰性渐变与彩色背景块。

**Key Characteristics:**

- 灰阶为体、电波青为睛:品牌色克制到只做"一声部"
- 平为默认、影随状态:1px 描边分层,阴影只响应交互状态
- 高密度仪器面板:small 控件、4px 基网、等宽数字
- 签名元素是字标尾部的闪烁光标方块——终端气质一眼可辨
- 暗色独立取值,亮色暗色都是"正房"

## Colors

色板的性格:低彩度青灰铺底,电波青一点即亮,功能色与主色同明度同饱和度,谁也不抢谁。

### Primary

- **电波青 (Signal Teal)** (#0c8078):主行动按钮、激活菜单、链接、焦点环。亮主题取 teal-600 是因为白底对比度 4.80:1 过 WCAG AA;真正的"电光"区(teal-400/500)过不了 AA,只在暗色主色(#0faea2)与图形强调(BrandMark 渐变)上使用。hover 亮一档(teal-500),按下深一档(teal-700)。

### Neutral

- **面板纸灰** (#f7fafb):页面底色,青灰 50 级。
- **仪器表面白** (#ffffff):卡片、表格、抽屉的表面色,与页面底色拉开一层。
- **悬浮雾灰** (#eff4f5):行悬浮、菜单悬浮、浅分割。
- **常规描边** (#e0e8ea):控件与区块边界;浅描边 (#eff4f5) 专用于卡片。
- **墨色文本** (#0f1b20):主文本,青灰 900 级。
- **正文灰** (#324249):常规正文,青灰 700 级。
- **辅助灰** (#617379):次要信息、描述文字。
- **占位灰** (#91a3a8):占位符与禁用态文本。

### Functional

功能色与主色同为 600 级中调,只表达状态语义:

- **可用绿** (#059669):可用、成功状态。
- **警示琥珀** (#d97706):可逆的警示操作、异常提示。
- **危险红** (#dc2626):仅不可逆操作。
- **信息青灰** (#45565c):中性提示、次要状态(青灰 600 级)。

### Named Rules

**The 一声部 Rule.** 电波青在任何单屏上的占比不超过 10%——主行动、激活态、链接、焦点环,仅此而已。它的稀有正是它的音量。

**The 状态用色 Rule.** 功能色只表达状态语义(success 可用 / warning 可逆警示 / danger 不可逆 / info 中性),永远不做装饰。想要"好看的颜色",答案是灰阶深浅,不是功能色。

**The 危险红纪律 Rule.** danger 只给不可逆操作(删除、清理、屏蔽);同一视觉区域内 danger 按钮至多一个主显,其余收进下拉。

## Typography

**Display Font:** 系统无衬线栈(-apple-system / PingFang SC / Microsoft YaHei 等)
**Body Font:** 同栈——全站单一无衬线族,靠字重与灰阶分层
**Label/Mono Font:** ui-monospace 栈(SF Mono / Menlo / Consolas),字标、节点名、IP、UUID、YAML 等"机器语料"专用

**Character:** 无衬线负责"读",等宽负责"核"。等宽体是这套系统的第二声音:字标用它,数据用它,凡是需要逐字符比对的内容都用它。

### Hierarchy

- **Display** (700, 28px/32px, line-height 1):仪表盘统计、体检总分等主视觉大数字。**非标题禁用**——标题再大也用 headline。
- **Headline** (600, 20px, 1.3):页面标题,只出现在 PageHeader。
- **Title** (600, 16px, 1.4):卡片标题、抽屉标题、区块小标。
- **Body** (400, 14px, 1.5):正文基准,表格主要内容。
- **Label** (400, 13px/12px, 1.5):次要信息、描述、表内辅助列;12px 是下限,不再小。
- **Mono** (400, 14px):字标(700,尺寸以 em 随宿主等比)、表格数字(配 `font-variant-numeric: tabular-nums`)、代码与标识符。

### Named Rules

**The Display 不做标题 Rule.** 28px/32px 两档 display 字号只服务主视觉数字。页面标题永远 20px,再大的标题需求,答案是把数字做成 display,不是把标题放大。

**The 数字对齐 Rule.** 表格与统计中的数字一律 `tabular-nums`,延迟、速率、评分纵向扫读时个位对个位。

## Layout

三段式骨架:侧边栏(210px,折叠 64px)+ 顶栏(52px)+ 内容区。**没有多页签栏,没有面包屑**——页面定位由侧边栏分组(总览 / 资源 / 配置)与 PageHeader 标题承担,顶栏只放折叠、主题、全屏、用户菜单,不承载导航。

间距全部取自 4px 基网刻度(4/8/12/16/24/32/48/64):图标与文字间 4px,控件间 8px,卡片内边距与区块间 16px,页面级留白到 64px 封顶。不允许刻度外的自由发挥。

密度是设计决策而非妥协:控件一律 small(24px 高),表格行高紧凑,筛选区 change 即生效不设"查询"按钮。页面只有两种模式——**列表页**(PageHeader → 筛选区 → 上下文化批量栏 → 表格 → 分页)与**详情**(右侧抽屉是唯一详情容器,480px 纯字段 / 640px 含子表;dialog 只做表单操作,不做详情展示)。

响应式:≤768px 侧边栏改抽屉,折叠开关语义随之切换。

### Named Rules

**The 刻度外无间距 Rule.** 间距、圆角、z-index 只取令牌刻度,页面代码不允许出现刻度外的字面量。这条是规范纪律,靠评审执行;lint 硬门禁覆盖的是内联 style 与单文件行数。

**The 抽屉看、对话办 Rule.** el-drawer = 查看详情,el-dialog = 表单操作,两者语义不互换;表格 expand 展开行禁用。

## Elevation & Depth

这是一套**平为默认**的系统:表面静止时几乎无阴影,层级靠 1px 描边(卡片用浅描边 #eff4f5)与表面色差(纸灰底 vs 白表面)表达。阴影只作为状态响应出现:卡片 hover 微抬一档,抽屉/对话框/浮层用第三档确立前后关系。

### Shadow Vocabulary

- **静置微影** (`0 1px 2px rgba(15,27,32,0.05)`):卡片默认态,仅让白表面从纸灰底上"可触摸"。
- **悬浮抬升** (`0 2px 8px rgba(15,27,32,0.08)`):hover 响应、轻浮层。
- **浮层确立** (`0 8px 24px rgba(15,27,32,0.12)`):抽屉、对话框、下拉等覆盖层。

暗色下三档换成纯黑基底(0.3/0.4/0.5 透明度)——青灰阴影在暗底上不可见,这是独立取值而非反色。

### Named Rules

**The 影随状态 Rule.** 阴影是交互的答语,不是装饰的常态。一个元素没有发生状态变化,它就不该投出新的影子。

## Shapes

形语言是**仪器的倒角**:小到刚好不割手,大到绝不圆润可爱。控件 4px(按钮、输入框、标签外的几乎所有小件),卡片 8px,抽屉与浮层 12px,胶囊 999px 只给状态点、标签这类"读数"元素。中间档 6px 是默认值,实际使用面很窄——拿不准时,小件取 4px、容器取 8px。

签名几何有两件:字标尾部的**光标方块**(0.55em × 1em,主色填充,1.2s 步进闪烁,`prefers-reduced-motion` 下静止)与 **BrandMark**(圆角矩形 rx=14 内,hub 辐条 + P 弧的白色线稿,渐变取 teal-400→teal-700,favicon 与侧边栏同源)。除此之外不放任何与品牌色不一致的位图 logo。

## Components

### Buttons

仪器按键:小、准、反馈明确但安静——按下去你知道它工作了,但它不邀功。

- **Shape:** 微倒角(4px)
- **Primary:** 电波青底白字,small 尺寸(24px 高,5px 11px 内边距)
- **Hover / Focus:** hover 亮一档(teal-500),按下深一档(teal-700),焦点环主色;过渡统一 0.2s cubic-bezier(0.4, 0, 0.2, 1)
- **Default:** 白底 + 常规描边 + 正文灰文字,承载次要动作
- **Danger:** 仅不可逆操作,遵守"危险红纪律";可逆操作用 default 或 warning

### Cards / Containers

- **Corner Style:** 8px 圆角
- **Background:** 仪器表面白,浮在面板纸灰上
- **Shadow Strategy:** 静置微影,hover 抬升一档(见 Elevation)
- **Border:** 1px 浅描边(#eff4f5)
- **Internal Padding:** 16px(space-4)

### Inputs / Fields

- **Style:** 白底、1px 常规描边、4px 圆角、small(24px 高)
- **Focus:** 描边与焦点环转电波青,无发光、无色块填充
- **Error / Disabled:** 错误态 danger 描边 + danger 文案;禁用态灰底 + 占位灰文字

### Tags / Status

- **Tag:** 胶囊(999px),雾灰底 + 正文灰字,20px 高;语义着色时只染功能色浅底,不做实心彩块堆砌
- **StatusDot:** 8px 圆点,只取功能色(success/warning/danger/info)或占位灰(muted 未测/禁用);label 同时作 tooltip 与 aria-label,无 label 即纯装饰

### Navigation

侧边栏白底(暗色为表面色),菜单分**总览 / 资源 / 配置**三组,由路由 meta 派生;激活项电波青文字 + 浅青底,hover 雾灰底。导航图标一律 Tabler Icons(2px 描边),EP 图标只留在页面内部语境。顶栏 52px,右侧依次主题切换、全屏、用户菜单。

### PageHeader(页首版式)

所有内容页的第一块砖:左侧 headline 标题(默认取路由 meta.title)+ 可选一行 13px 辅助灰描述,右侧页面级主操作区(超过 2 个收进下拉)。新页面禁止自写标题行。

### Wordmark(签名组件)

等宽粗体 "ProxyHub" + 主色光标方块,闪烁 1.2s 步进。字号以 em 为基准,宿主 font-size 控制等比缩放:侧边栏 16px,登录页与 Setup 页 28px。这是系统的签名——终端气质的来源,任何品牌位都优先用它与 BrandMark,不用第三套标志。

### Drawer / Dialog 分工

详情抽屉右侧滑出,480px(纯字段)或 640px(含子表);对话框只做表单与确认。抽屉内只允许轻量行内操作,不嵌大表单。

## Do's and Don'ts

### Do:

- **Do** 只消费语义令牌(`--ph-bg-*` / `--ph-text-*` / `--ph-color-*`);调值去 tokens.css,调暗色观感去 semantics.css,调 EP 组件去 ep-theme.css。
- **Do** 让电波青保持稀有——主行动、激活态、链接、焦点环,单屏 ≤10%。
- **Do** 表格数字用等宽 + `tabular-nums`;主视觉大数字用 display 档(700 字重)。
- **Do** 新页面先归入列表页或详情模式,页首接 PageHeader,详情走右侧抽屉。
- **Do** 暗色作为一等公民同步设计:只覆盖语义层,页面代码零改动即切换。
- **Do** 危险操作走 danger + 确认;不可逆语义与红色绑定。

### Don't:

- **Don't** 在页面代码写原始刻度(`--ph-teal-600`)、`--el-*` 变量或内联 style——内联 style 由 lint 门禁硬阻塞,原始刻度与 `--el-*` 禁令是令牌三层架构的纪律。
- **Don't** 用功能色做装饰,或让 danger 出现在可逆操作上。
- **Don't** 引入渐变、大圆角、彩色背景块;BrandMark 的 teal 渐变是唯一例外,且仅限品牌位。
- **Don't** 用 dialog 展示详情、用 expand 展开行、自写页面标题行。
- **Don't** 把 display 字号(28/32px)用在标题上,或写出 12px 以下的文字。
- **Don't** 放与电波青不一致的位图 logo,或自造字标的替代品。
