package nodetag

import (
	"reflect"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// contains 报告 tags 是否含 want。
func contains(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// TestDerive_NoData 无检测结果且无体检报告的节点派生零标签(非报错、非 nil-panic)。
func TestDerive_NoData(t *testing.T) {
	got := Derive(nil, nil)
	if len(got) != 0 {
		t.Fatalf("no-data node should derive zero tags, got %v", got)
	}
}

// TestDerive_UnlockLevels 解锁标签按 kind + level 派生;originals_only 与 full 区分,blocked 不出标签。
func TestDerive_UnlockLevels(t *testing.T) {
	cases := []struct {
		name    string
		unlocks []UnlockResult
		want    []string // 期望包含的标签(顺序无关)
		absent  []string // 期望不含的标签
	}{
		{
			name:    "netflix full",
			unlocks: []UnlockResult{{Kind: detection.KindNetflix, Level: detection.LevelFull}},
			want:    []string{"nf-full"},
			absent:  []string{"nf-originals"},
		},
		{
			name:    "netflix originals only",
			unlocks: []UnlockResult{{Kind: detection.KindNetflix, Level: detection.LevelOriginalsOnly}},
			want:    []string{"nf-originals"},
			absent:  []string{"nf-full"},
		},
		{
			name:    "netflix blocked yields nothing",
			unlocks: []UnlockResult{{Kind: detection.KindNetflix, Level: detection.LevelBlocked}},
			absent:  []string{"nf-full", "nf-originals"},
		},
		{
			name: "streaming and ai full",
			unlocks: []UnlockResult{
				{Kind: detection.KindYouTubePremium, Level: detection.LevelFull},
				{Kind: detection.KindDisneyPlus, Level: detection.LevelFull},
				{Kind: detection.KindOpenAI, Level: detection.LevelFull},
				{Kind: detection.KindClaude, Level: detection.LevelFull},
				{Kind: detection.KindGemini, Level: detection.LevelFull},
			},
			want: []string{"yt-premium", "disney-plus", "openai", "claude", "gemini"},
		},
		{
			name:    "openai blocked yields nothing",
			unlocks: []UnlockResult{{Kind: detection.KindOpenAI, Level: detection.LevelBlocked}},
			absent:  []string{"openai"},
		},
		{
			name:    "generic kind ignored",
			unlocks: []UnlockResult{{Kind: detection.KindGeneric, Level: ""}},
			absent:  []string{"nf-full", "openai"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(tc.unlocks, nil)
			for _, w := range tc.want {
				if !contains(got, w) {
					t.Errorf("want tag %q in %v", w, got)
				}
			}
			for _, a := range tc.absent {
				if contains(got, a) {
					t.Errorf("tag %q must be absent, got %v", a, got)
				}
			}
		})
	}
}

// TestDerive_StabilityBoundaries 稳定性分临界 79/80/60 精确落档。
func TestDerive_StabilityBoundaries(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{100, "stable-good"},
		{80, "stable-good"}, // 80 为 good 下界(含)
		{79, "stable-fair"}, // 79 落 fair
		{60, "stable-fair"}, // 60 为 fair 下界(含)
		{59, "stable-poor"}, // 59 落 poor
		{0, "stable-poor"},
	}
	for _, tc := range cases {
		report := &detection.ExamReport{
			Stability: &detection.StabilityMetrics{Total: 30, Succeeded: 30, Score: tc.score},
		}
		got := Derive(nil, report)
		if !contains(got, tc.want) {
			t.Errorf("score %d: want %q in %v", tc.score, tc.want, got)
		}
		// 三档互斥:只应命中一档。
		grades := 0
		for _, g := range []string{"stable-good", "stable-fair", "stable-poor"} {
			if contains(got, g) {
				grades++
			}
		}
		if grades != 1 {
			t.Errorf("score %d: expected exactly 1 quality grade, got %d in %v", tc.score, grades, got)
		}
	}
}

// TestDerive_StabilityNoSamples 体检有稳定性段但零样本(Total=0)不派生质量档(无数据非 stable-poor)。
func TestDerive_StabilityNoSamples(t *testing.T) {
	report := &detection.ExamReport{Stability: &detection.StabilityMetrics{Total: 0, Score: 0}}
	got := Derive(nil, report)
	for _, g := range []string{"stable-good", "stable-fair", "stable-poor"} {
		if contains(got, g) {
			t.Errorf("zero-sample stability must not derive %q, got %v", g, got)
		}
	}
}

// TestDerive_FastBaseline fast 取基准行下行 >=50Mbps;基准缺失/低速/错误不出 fast。
func TestDerive_FastBaseline(t *testing.T) {
	fast := &detection.ExamReport{RegionSpeed: &detection.RegionSpeedMetrics{Regions: []detection.RegionResult{
		{Code: "baseline", Name: "基准", DownMbps: 50.0},
		{Code: "US", DownMbps: 5.0},
	}}}
	if got := Derive(nil, fast); !contains(got, "fast") {
		t.Errorf("baseline 50Mbps should yield fast, got %v", got)
	}

	slow := &detection.ExamReport{RegionSpeed: &detection.RegionSpeedMetrics{Regions: []detection.RegionResult{
		{Code: "baseline", DownMbps: 49.9},
	}}}
	if got := Derive(nil, slow); contains(got, "fast") {
		t.Errorf("baseline 49.9Mbps must not be fast, got %v", got)
	}

	errored := &detection.ExamReport{RegionSpeed: &detection.RegionSpeedMetrics{Regions: []detection.RegionResult{
		{Code: "baseline", DownMbps: 0, Error: "timeout"},
		{Code: "US", DownMbps: 120.0}, // 非基准高速不算 fast
	}}}
	if got := Derive(nil, errored); contains(got, "fast") {
		t.Errorf("errored baseline must not be fast, got %v", got)
	}
}

// TestDerive_Egress 出网标签:region/ipv6/hosting/residential/dns-leak。
func TestDerive_Egress(t *testing.T) {
	report := &detection.ExamReport{Egress: &detection.EgressMetrics{
		IPv4: &detection.EgressIPv4{IP: "203.0.113.5", CountryCode: "us", Hosting: true},
		IPv6: &detection.EgressIPv6{Available: true, Address: "2001:db8::1"},
		DNS:  &detection.EgressDNS{Leak: true},
	}}
	got := Derive(nil, report)
	for _, w := range []string{"region:US", "ipv6", "hosting", "dns-leak"} {
		if !contains(got, w) {
			t.Errorf("want %q in %v", w, got)
		}
	}
	// hosting 与 residential 互斥。
	if contains(got, "residential") {
		t.Errorf("hosting IP must not also be residential: %v", got)
	}
}

// TestDerive_EgressResidential 非机房 IP 派生 residential;region 国家码大写。
func TestDerive_EgressResidential(t *testing.T) {
	report := &detection.ExamReport{Egress: &detection.EgressMetrics{
		IPv4: &detection.EgressIPv4{IP: "198.51.100.7", CountryCode: "JP", Hosting: false},
	}}
	got := Derive(nil, report)
	if !contains(got, "residential") {
		t.Errorf("non-hosting IP should be residential: %v", got)
	}
	if !contains(got, "region:JP") {
		t.Errorf("want region:JP in %v", got)
	}
	if contains(got, "hosting") {
		t.Errorf("non-hosting IP must not be hosting: %v", got)
	}
}

// TestDerive_EgressMissingFields 出网字段缺失(IPv4 探测失败/IPv6 不可用/无 DNS)不误派生标签。
func TestDerive_EgressMissingFields(t *testing.T) {
	report := &detection.ExamReport{Egress: &detection.EgressMetrics{
		IPv4: &detection.EgressIPv4{Error: "probe failed"}, // 无 IP/国家码
		IPv6: &detection.EgressIPv6{Available: false},
		DNS:  &detection.EgressDNS{Leak: false},
	}}
	got := Derive(nil, report)
	for _, a := range []string{"ipv6", "hosting", "residential", "dns-leak"} {
		if contains(got, a) {
			t.Errorf("missing/failed egress must not derive %q, got %v", a, got)
		}
	}
	for _, tag := range got {
		if len(tag) >= 7 && tag[:7] == "region:" {
			t.Errorf("empty country code must not derive %q", tag)
		}
	}
}

// TestDerive_SortedUnique 输出稳定排序且去重(同一 kind 不会因重复输入出现重复标签)。
func TestDerive_SortedUnique(t *testing.T) {
	unlocks := []UnlockResult{
		{Kind: detection.KindNetflix, Level: detection.LevelFull},
		{Kind: detection.KindNetflix, Level: detection.LevelFull}, // 重复
	}
	got := Derive(unlocks, nil)
	if !reflect.DeepEqual(got, []string{"nf-full"}) {
		t.Errorf("duplicate input should collapse to single sorted tag, got %v", got)
	}
}
