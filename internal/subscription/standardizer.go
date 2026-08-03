package subscription

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// DefaultNameTemplate 默认节点名称模板:国旗 地区 机场简称-序号(如 🇭🇰 香港 JS-01)。
const DefaultNameTemplate = "{emoji} {region} {source_abbr}-{index}"

// RegionInfo 地区展示信息:中文名 + 国旗 emoji。供节点名称标准化取值。
type RegionInfo struct {
	Code  string // HK
	Name  string // 香港
	Emoji string // 🇭🇰
}

// Standardizer 节点名称标准化器。按名称模板把机场原名转换为统一格式(见 ADR 0012)。
//
// 模板变量:{emoji} {region} {region_code} {source} {source_abbr} {index} {original_name}
type Standardizer struct {
	template     string
	airportAbbrs map[string]string     // 机场名 → 简称
	regions      map[string]RegionInfo // 地区代码 → 展示信息
}

// NewStandardizer 创建标准化器。airportAbbrs 为机场名到简称的映射,
// regions 为地区代码到展示信息的映射。
func NewStandardizer(template string, airportAbbrs map[string]string, regions map[string]RegionInfo) *Standardizer {
	return &Standardizer{
		template:     template,
		airportAbbrs: airportAbbrs,
		regions:      regions,
	}
}

// Format 按模板生成单个节点的标准化名称。index 为组内序号(从 1 开始)。
func (s *Standardizer) Format(node *Node, index int) string {
	region := node.Region
	emoji := ""
	if info, ok := s.regions[node.Region]; ok {
		region = info.Name
		emoji = info.Emoji
	} else if node.Region == "Unknown" {
		// 地区识别不到:emoji 用地球,region 用去噪首段,空则回退机场简称
		emoji = "🌐"
		if name := extractKeyName(node.Name); name != "" {
			region = name
		} else {
			region = s.airportAbbrs[node.Source]
		}
	} else {
		// 其它未登记地区码:emoji 由代码计算,region 名回退为代码本身
		emoji = RegionEmoji(node.Region)
		region = node.Region
	}

	replacer := strings.NewReplacer(
		"{emoji}", emoji,
		"{region}", region,
		"{region_code}", node.Region,
		"{source}", node.Source,
		"{source_abbr}", s.airportAbbrs[node.Source],
		"{index}", fmt.Sprintf("%02d", index),
		"{original_name}", node.Name,
	)
	return replacer.Replace(s.template)
}

// StandardizeNodes 对整池节点计算标准化名称,返回带 DisplayName 的新切片。
//
// 分组规则:按 (机场, 地区) 分组,组内按 NodeKey 稳定排序后从 1 编号
// (见 ADR 0012:按 NodeKey 排序保证序号不随延迟波动)。
// 不修改入参节点(immutability):每个节点浅拷贝后填充 DisplayName。
func (s *Standardizer) StandardizeNodes(nodes []*Node) []*Node {
	// 分组:key = source + "\x00" + region
	groups := make(map[string][]*Node)
	order := make([]string, 0) // 保留分组首次出现顺序,输出稳定
	for _, n := range nodes {
		key := n.Source + "\x00" + n.Region
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], n)
	}

	result := make([]*Node, 0, len(nodes))
	for _, key := range order {
		group := groups[key]

		// 组内按 NodeKey 排序,保证序号稳定
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].NodeKey() < group[j].NodeKey()
		})
		for idx, n := range group {
			cp := *n // 浅拷贝,避免修改入参
			cp.DisplayName = s.Format(n, idx+1)
			result = append(result, &cp)
		}
	}
	return result
}

// extractKeyName 从机场原名去噪后取首个有意义段,供地区识别不到时替代 "Unknown"。
// 规则:剥离前导国旗 emoji / 符号 / 空白;从首个字母或 CJK 起,取到分隔符、数字或符号前。
// 全为符号/空白时返回空串(调用方回退机场简称)。
func extractKeyName(raw string) string {
	isCJK := func(r rune) bool {
		return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
	}
	meaningful := func(r rune) bool { return unicode.IsLetter(r) || isCJK(r) }

	runes := []rune(raw)
	// 跳到首个有意义字符
	i := 0
	for i < len(runes) && !meaningful(runes[i]) {
		i++
	}
	if i >= len(runes) {
		return ""
	}
	// 取到首个数字或非(字母/CJK/空格)符号前;允许中间单空格
	var sb strings.Builder
	for ; i < len(runes); i++ {
		r := runes[i]
		if meaningful(r) || r == ' ' {
			sb.WriteRune(r)
			continue
		}
		break
	}
	return strings.TrimSpace(sb.String())
}

// RegionCodeFromEmoji 国旗 emoji 反解(RegionEmoji 正向算法可逆,issue #37 第二层兜底):
// 从左到右扫描名称中的区域指示符对(U+1F1E6..U+1F1FF x2),把第一面旗反解为两位
// ISO 代码。多面旗确定取第一个(入口标注习惯);非法/落单指示符忽略;无国旗返回空串。
func RegionCodeFromEmoji(name string) string {
	runes := []rune(name)
	for i := 0; i+1 < len(runes); i++ {
		a, b := runes[i], runes[i+1]
		if a < 0x1F1E6 || a > 0x1F1FF {
			continue
		}
		if b < 0x1F1E6 || b > 0x1F1FF {
			continue // 落单指示符:忽略它,继续向后扫描
		}
		return string(rune('A'+a-0x1F1E6)) + string(rune('A'+b-0x1F1E6))
	}
	return ""
}

// RegionEmoji 把两位地区代码转换为国旗 emoji(区域指示符),如 HK → 🇭🇰。
// 非两位字母的代码(Unknown、空等)返回地球 🌐。
func RegionEmoji(code string) string {
	if len(code) != 2 {
		return "🌐"
	}
	var sb strings.Builder
	for _, r := range strings.ToUpper(code) {
		if r < 'A' || r > 'Z' {
			return "🌐"
		}
		// 区域指示符 A = U+1F1E6
		sb.WriteRune(0x1F1E6 + (r - 'A'))
	}
	return sb.String()
}
