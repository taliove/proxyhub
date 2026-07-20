// Package nodetag 从节点测试结果派生自动标签(纯函数,无 IO/DB 依赖)。
// 领域位置在 store 之上:store 编排读取(node_health 最新解锁 + 最近体检报告)后
// 调用 Derive 得到标签集合再落库。规则表见本文件常量与 unlockRules。
package nodetag

import (
	"sort"
	"strings"

	"github.com/taliove/proxyhub/internal/detection"
)

// UnlockResult 派生输入:单个解锁目标的最新判定(已由编排层把 target_name 解析为 Kind)。
// generic 目标(连通性/带宽)不构成解锁输入,编排层不应传入。
type UnlockResult struct {
	Kind  detection.Kind // 专用解锁 kind(netflix/openai/...)
	Level string         // 解锁级别:full/originals_only/blocked(空视为无结论)
}

// 质量档分数临界(稳定性分 0..100):good 下界含 80,fair 区间 [60,79]。
const (
	stableGoodMin = 80 // >=80 -> stable-good
	stableFairMin = 60 // [60,79] -> stable-fair,<60 -> stable-poor
)

// fastBaselineMinMbps fast 标签的基准下行门槛(Mbps,含)。
const fastBaselineMinMbps = 50.0

// baselineRegionCode 多地域测速的基准对照行代码,与
// internal/detection/exam_regionspeed.go 的 baselineRegion.Code 保持一致。
const baselineRegionCode = "baseline"

// unlockRules 解锁规则表:(kind, level) -> tag。一条命中出一个标签,blocked/空级别不出标签。
// 新增服务只加一行,不改派生逻辑。
var unlockRules = []struct {
	kind  detection.Kind
	level string
	tag   string
}{
	{detection.KindNetflix, detection.LevelFull, "nf-full"},
	{detection.KindNetflix, detection.LevelOriginalsOnly, "nf-originals"},
	{detection.KindYouTubePremium, detection.LevelFull, "yt-premium"},
	{detection.KindDisneyPlus, detection.LevelFull, "disney-plus"},
	{detection.KindOpenAI, detection.LevelFull, "openai"},
	{detection.KindClaude, detection.LevelFull, "claude"},
	{detection.KindGemini, detection.LevelFull, "gemini"},
}

// Derive 从最新解锁判定 + 最近体检报告派生标签集合。
// 纯函数:相同输入恒得相同输出;无数据(nil/空)返回空切片而非报错。
// 输出去重并按字典序稳定排序,便于落库与断言。
func Derive(unlocks []UnlockResult, report *detection.ExamReport) []string {
	set := make(map[string]struct{})

	deriveUnlockTags(unlocks, set)
	deriveEgressTags(report, set)
	deriveQualityTags(report, set)

	tags := make([]string, 0, len(set))
	for t := range set {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// deriveUnlockTags 按规则表派生解锁标签。
func deriveUnlockTags(unlocks []UnlockResult, set map[string]struct{}) {
	for _, u := range unlocks {
		for _, rule := range unlockRules {
			if u.Kind == rule.kind && u.Level == rule.level {
				set[rule.tag] = struct{}{}
			}
		}
	}
}

// deriveEgressTags 从体检报告出网段派生 region:<CC>/ipv6/hosting/residential/dns-leak。
func deriveEgressTags(report *detection.ExamReport, set map[string]struct{}) {
	if report == nil || report.Egress == nil {
		return
	}
	e := report.Egress

	// IPv4:有效出口(探测成功且拿到 IP)才派生 region 与 hosting/residential。
	if v4 := e.IPv4; v4 != nil && v4.Error == "" && v4.IP != "" {
		if cc := strings.ToUpper(strings.TrimSpace(v4.CountryCode)); cc != "" {
			set["region:"+cc] = struct{}{}
		}
		// hosting 与 residential 互斥:机房 IP -> hosting,否则 residential。
		if v4.Hosting {
			set["hosting"] = struct{}{}
		} else {
			set["residential"] = struct{}{}
		}
	}

	if e.IPv6 != nil && e.IPv6.Available {
		set["ipv6"] = struct{}{}
	}

	if e.DNS != nil && e.DNS.Leak {
		set["dns-leak"] = struct{}{}
	}
}

// deriveQualityTags 从体检报告派生稳定性档(stable-good/fair/poor)与 fast。
func deriveQualityTags(report *detection.ExamReport, set map[string]struct{}) {
	if report == nil {
		return
	}

	// 稳定性:仅在体检确有采样(Total>0)时落档;无数据不派生(避免 score 0 误判 poor)。
	if s := report.Stability; s != nil && s.Total > 0 {
		switch {
		case s.Score >= stableGoodMin:
			set["stable-good"] = struct{}{}
		case s.Score >= stableFairMin:
			set["stable-fair"] = struct{}{}
		default:
			set["stable-poor"] = struct{}{}
		}
	}

	// fast:基准对照行下行达标(基准行代表就近 POP 的上限参考)。
	if rs := report.RegionSpeed; rs != nil {
		for _, r := range rs.Regions {
			if r.Code == baselineRegionCode && r.Error == "" && r.DownMbps >= fastBaselineMinMbps {
				set["fast"] = struct{}{}
				break
			}
		}
	}
}
