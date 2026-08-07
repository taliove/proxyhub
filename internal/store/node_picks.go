package store

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// 订阅地址精选 node_picks 的对象形态(spec #84 / issue #85):
// 第一版(issue #79)为纯 NodeKey 字符串数组;升级为 {key, alias} 对象数组后,
// alias 成为该订阅下发命名链的最终层(仅本订阅生效,不回写节点池)。
// 存储列不变(仍是 endpoints.node_picks 原始 JSON 文本),读取双格式兼容,
// 写入一律新格式——存量数据零迁移、零行为变化。

// NodePick 精选项:Key = NodeKey(精选按它记忆,机场改名仍命中);
// Alias 可选,留空 = 跟随命名链(标准化/改名覆盖 -> 订阅级模板)。
type NodePick struct {
	Key   string `json:"key"`
	Alias string `json:"alias,omitempty"`
}

// UnmarshalJSON 双格式兼容:JSON 字符串 -> 仅 Key(旧格式);
// 对象 -> key/alias;其余形状(数字/布尔/null/嵌套)报错,
// 由边界校验拒绝落库(与旧版"必须字符串数组"的形状校验同哲学)。
func (p *NodePick) UnmarshalJSON(data []byte) error {
	var key string
	if err := json.Unmarshal(data, &key); err == nil {
		p.Key, p.Alias = key, ""
		return nil
	}
	var obj struct {
		Key   string `json:"key"`
		Alias string `json:"alias"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("node pick must be a NodeKey string or {key, alias} object")
	}
	// 对象分支必须带非空 key:旧版 []string 解码遇 {} 元素直接报错
	// (读侧 WARN 降级为全量、写侧拒收);静默接受为空 Key 惰性项会把
	// "形状错宁可全量"的降级哲学变成"静默收窄下发",parity 必须保持。
	// ([null] 走上面的字符串分支解析为空 Key,与旧版 []string 行为一致,不受影响。)
	if obj.Key == "" {
		return fmt.Errorf("node pick object requires non-empty key")
	}
	p.Key, p.Alias = obj.Key, obj.Alias
	return nil
}

// ParseNodePicks 解析精选原始 JSON:空串 = 未配置(nil, nil);
// 新旧格式混合数组均可解析;非法 JSON / 非法元素形状返回 error
// (读取侧按"宁可全量"降级,见 server.endpointNodePicks)。
func ParseNodePicks(raw string) ([]NodePick, error) {
	if raw == "" {
		return nil, nil
	}
	var picks []NodePick
	if err := json.Unmarshal([]byte(raw), &picks); err != nil {
		return nil, err
	}
	return picks, nil
}

// maxPickAliasRunes 精选项别名 rune 上限:展示卫生(与公开名称 maxPublicNameRunes
// 同例),防超长串撑爆客户端配置列表;不是安全闸门(生成链对名称整体编码)。
const maxPickAliasRunes = 50

// SanitizeNodePickAlias 别名边界归一:trim 首尾空白、剔除控制字符、
// 截断到 maxPickAliasRunes。空串合法(=无别名,跟随命名链)。
func SanitizeNodePickAlias(alias string) string {
	return sanitizeDisplayText(alias, maxPickAliasRunes)
}

// sanitizeDisplayText 展示型短文本的统一归一:trim、去控制字符、按 rune 截断。
// 公开名称(issue #38)与精选项别名(issue #85)共用,均在边界处归一,不让脏数据落库。
func sanitizeDisplayText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	runes := []rune(b.String())
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}
