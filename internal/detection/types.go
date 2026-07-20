package detection

// Target 检测目标配置(解锁检测的探测点)
type Target struct {
	Name     string            `json:"name"`      // 目标名称(如 "OpenAI", "YouTube")
	URL      string            `json:"url"`       // 探测 URL
	Method   string            `json:"method"`    // HTTP 方法(GET/POST)
	Headers  map[string]string `json:"headers"`   // 自定义 headers
	ExpectStatus  []int    `json:"expect_status"`    // 期望状态码列表(命中任一即通过)
	ResponseContains []string `json:"response_contains"` // 响应体必须包含的关键字(全部命中才通过,空=不检查)
	ResponseExcludes []string `json:"response_excludes"` // 响应体不得包含的关键字(命中任一即失败,空=不检查)
}

// Result 单个节点对单个目标的检测结果
type Result struct {
	NodeKey    string `json:"node_key"`
	TargetName string `json:"target_name"`
	Available  bool   `json:"available"`
	Latency    int    `json:"latency"`    // 毫秒
	Error      string `json:"error,omitempty"` // 失败原因
}

// TestResult 单节点即时测试结果(quick/real/bandwidth 三档共用)。
type TestResult struct {
	Available bool   `json:"available"`
	Latency   int    `json:"latency"` // 毫秒
	Mode      string `json:"mode"`    // "quick" / "real" / "bandwidth"
	Error     string `json:"error,omitempty"`
	// 带宽测试结果(仅 mode=bandwidth 时非零)
	DownMbps    float64 `json:"down_mbps,omitempty"`
	UpMbps      float64 `json:"up_mbps,omitempty"`
	ElapsedMs   int     `json:"elapsed_ms,omitempty"`     // 带宽测试总耗时(毫秒)
	MinDownMbps float64 `json:"min_down_mbps,omitempty"`  // 下行合格阈值(供 UI 展示)
	MinUpMbps   float64 `json:"min_up_mbps,omitempty"`    // 上行合格阈值
}

