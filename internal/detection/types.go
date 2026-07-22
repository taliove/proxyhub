package detection

// Kind 检测目标类型:generic(通用状态码/关键字判定)或专用解锁判定(netflix 等)。
// 空值等价于 KindGeneric,序列化时省略,保证既有 generic 目标的 API 响应零变化。
type Kind string

// 六种解锁 kind 常量 + generic。专用 kind 的判定逻辑由后续票据(02-04)填充,
// 本票据只建骨架:未知 kind 必须明确报错,不得静默按 generic 处理。
const (
	KindGeneric        Kind = "generic"
	KindNetflix        Kind = "netflix"
	KindYouTubePremium Kind = "youtube_premium"
	KindDisneyPlus     Kind = "disney_plus"
	KindOpenAI         Kind = "openai"
	KindClaude         Kind = "claude"
	KindGemini         Kind = "gemini"
)

// 解锁级别常量:专用判定填充 Result.Level(generic 判定不填充)。
const (
	LevelFull          = "full"           // 完整解锁
	LevelOriginalsOnly = "originals_only" // 仅自制剧(Netflix 等)
	LevelBlocked       = "blocked"        // 被封锁
)

// Target 检测目标配置(解锁检测的探测点)
type Target struct {
	Name             string            `json:"name"`              // 目标名称(如 "OpenAI", "YouTube")
	Kind             Kind              `json:"kind,omitempty"`    // 检测类型:空/generic=通用判定,其余=专用解锁判定
	URL              string            `json:"url"`               // 探测 URL
	Method           string            `json:"method"`            // HTTP 方法(GET/POST)
	Headers          map[string]string `json:"headers"`           // 自定义 headers
	ExpectStatus     []int             `json:"expect_status"`     // 期望状态码列表(命中任一即通过)
	ResponseContains []string          `json:"response_contains"` // 响应体必须包含的关键字(全部命中才通过,空=不检查)
	ResponseExcludes []string          `json:"response_excludes"` // 响应体不得包含的关键字(命中任一即失败,空=不检查)
}

// Result 单个节点对单个目标的检测结果
type Result struct {
	NodeKey    string `json:"node_key"`
	TargetName string `json:"target_name"`
	Available  bool   `json:"available"`
	Latency    int    `json:"latency"`         // 毫秒
	Error      string `json:"error,omitempty"` // 失败原因
	// Level/Region 仅专用解锁判定填充;generic 结果留空,序列化省略,保证响应零变化。
	Level  string `json:"level,omitempty"`  // 解锁级别:full/originals_only/blocked
	Region string `json:"region,omitempty"` // 命中区域国家码(如 US/HK)
	// cause 传输失败的结构化底层错误(仅请求类失败填充;判定结论/解析失败为 nil)。
	// 不序列化,供解锁段重试分类器经 errors.Is/As 结构化判定(文本 Error 退化为兜底)。
	cause error
}

// TestResult 单节点即时测试结果(quick/real/bandwidth 三档共用)。
type TestResult struct {
	Available bool   `json:"available"`
	Latency   int    `json:"latency"` // 毫秒
	Mode      string `json:"mode"`    // "quick" / "real" / "bandwidth"
	Error     string `json:"error,omitempty"`
	// FailReason 失败原因分类(FailReason* 枚举,ticket 0017);成功时为空。
	// Error 是自由文本详情,FailReason 是有限枚举——列表/诊断展示以分类为准。
	FailReason string `json:"fail_reason,omitempty"`
	// 带宽测试结果(仅 mode=bandwidth 时非零)
	DownMbps    float64 `json:"down_mbps,omitempty"`
	UpMbps      float64 `json:"up_mbps,omitempty"`
	ElapsedMs   int     `json:"elapsed_ms,omitempty"`    // 带宽测试总耗时(毫秒)
	MinDownMbps float64 `json:"min_down_mbps,omitempty"` // 下行合格阈值(供 UI 展示)
	MinUpMbps   float64 `json:"min_up_mbps,omitempty"`   // 上行合格阈值
}
