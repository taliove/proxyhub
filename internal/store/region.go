package store

import (
	"fmt"
	"strings"

	"github.com/taliove/proxyhub/internal/subscription"
)

// RegionRule 地区识别规则
type RegionRule struct {
	RegionCode string // HK, SG, US, ...
	RegionName string // 香港, 新加坡, 美国, ...
	Pattern    string // 匹配模式: "香港", "HK", "🇭🇰"
	Priority   int    // 优先级（越大越优先）
}

// InitRegionRules 初始化地区识别规则表，并预置常见地区规则（参考 subconverter）。
// 优先级: 100=国旗emoji, 80=中文全名, 60=英文全名, 40=缩写, 20=别名
func (s *Store) InitRegionRules() error {
	// 建表
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS region_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			region_code TEXT NOT NULL,
			region_name TEXT NOT NULL,
			pattern TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			UNIQUE(region_code, pattern)
		)
	`)
	if err != nil {
		return fmt.Errorf("create region_rules table: %w", err)
	}

	// 预置规则（常见地区）。始终以 INSERT OR IGNORE 幂等写入:
	// 既能初始化空库,也能给既有安装补齐新增地区(靠 UNIQUE(region_code,pattern) 去重),
	// 且不覆盖用户可能已改动的行(见 ADR 0013 地区映射补全)。
	rules := []RegionRule{
		// 香港
		{"HK", "香港", "🇭🇰", 100},
		{"HK", "香港", "香港", 80},
		{"HK", "香港", "Hong Kong", 60},
		{"HK", "香港", "HK", 40},
		{"HK", "香港", "港", 40},

		// 新加坡
		{"SG", "新加坡", "🇸🇬", 100},
		{"SG", "新加坡", "新加坡", 80},
		{"SG", "新加坡", "Singapore", 60},
		{"SG", "新加坡", "SG", 40},
		{"SG", "新加坡", "狮城", 40},
		{"SG", "新加坡", "新", 20},

		// 美国
		{"US", "美国", "🇺🇸", 100},
		{"US", "美国", "美国", 80},
		{"US", "美国", "United States", 60},
		{"US", "美国", "US", 40},
		{"US", "美国", "USA", 40},
		{"US", "美国", "美", 20},

		// 日本
		{"JP", "日本", "🇯🇵", 100},
		{"JP", "日本", "日本", 80},
		{"JP", "日本", "Japan", 60},
		{"JP", "日本", "JP", 40},
		{"JP", "日本", "日", 20},

		// 台湾
		{"TW", "台湾", "🇹🇼", 100},
		{"TW", "台湾", "台湾", 80},
		{"TW", "台湾", "Taiwan", 60},
		{"TW", "台湾", "TW", 40},
		{"TW", "台湾", "台", 20},

		// 韩国
		{"KR", "韩国", "🇰🇷", 100},
		{"KR", "韩国", "韩国", 80},
		{"KR", "韩国", "Korea", 60},
		{"KR", "韩国", "KR", 40},
		{"KR", "韩国", "韩", 20},

		// 英国
		{"GB", "英国", "🇬🇧", 100},
		{"GB", "英国", "英国", 80},
		{"GB", "英国", "United Kingdom", 60},
		{"GB", "英国", "UK", 40},
		{"GB", "英国", "GB", 40},
		{"GB", "英国", "英", 20},

		// 德国
		{"DE", "德国", "🇩🇪", 100},
		{"DE", "德国", "德国", 80},
		{"DE", "德国", "Germany", 60},
		{"DE", "德国", "DE", 40},
		{"DE", "德国", "德", 20},

		// 法国
		{"FR", "法国", "🇫🇷", 100},
		{"FR", "法国", "法国", 80},
		{"FR", "法国", "France", 60},
		{"FR", "法国", "FR", 40},
		{"FR", "法国", "法", 20},

		// 加拿大
		{"CA", "加拿大", "🇨🇦", 100},
		{"CA", "加拿大", "加拿大", 80},
		{"CA", "加拿大", "Canada", 60},
		{"CA", "加拿大", "CA", 40},
		{"CA", "加拿大", "加", 20},

		// 澳大利亚
		{"AU", "澳大利亚", "🇦🇺", 100},
		{"AU", "澳大利亚", "澳大利亚", 80},
		{"AU", "澳大利亚", "澳洲", 80},
		{"AU", "澳大利亚", "Australia", 60},
		{"AU", "澳大利亚", "AU", 40},
		{"AU", "澳大利亚", "澳", 20},

		// 印度
		{"IN", "印度", "🇮🇳", 100},
		{"IN", "印度", "印度", 80},
		{"IN", "印度", "India", 60},
		{"IN", "印度", "Mumbai", 60},
		{"IN", "印度", "IN", 40},

		// 俄罗斯
		{"RU", "俄罗斯", "🇷🇺", 100},
		{"RU", "俄罗斯", "俄罗斯", 80},
		{"RU", "俄罗斯", "Russia", 60},
		{"RU", "俄罗斯", "Moscow", 60},
		{"RU", "俄罗斯", "RU", 40},
		{"RU", "俄罗斯", "俄", 20},

		// 荷兰
		{"NL", "荷兰", "🇳🇱", 100},
		{"NL", "荷兰", "荷兰", 80},
		{"NL", "荷兰", "Netherlands", 60},
		{"NL", "荷兰", "Holland", 60},
		{"NL", "荷兰", "Amsterdam", 60},
		{"NL", "荷兰", "NL", 40},
		{"NL", "荷兰", "荷", 20},

		// 土耳其
		{"TR", "土耳其", "🇹🇷", 100},
		{"TR", "土耳其", "土耳其", 80},
		{"TR", "土耳其", "Turkey", 60},
		{"TR", "土耳其", "Istanbul", 60},
		{"TR", "土耳其", "TR", 40},

		// 菲律宾
		{"PH", "菲律宾", "🇵🇭", 100},
		{"PH", "菲律宾", "菲律宾", 80},
		{"PH", "菲律宾", "Philippines", 60},
		{"PH", "菲律宾", "Manila", 60},
		{"PH", "菲律宾", "PH", 40},
		{"PH", "菲律宾", "菲", 20},

		// 泰国
		{"TH", "泰国", "🇹🇭", 100},
		{"TH", "泰国", "泰国", 80},
		{"TH", "泰国", "Thailand", 60},
		{"TH", "泰国", "Bangkok", 60},
		{"TH", "泰国", "TH", 40},
		{"TH", "泰国", "泰", 20},

		// 阿根廷
		{"AR", "阿根廷", "🇦🇷", 100},
		{"AR", "阿根廷", "阿根廷", 80},
		{"AR", "阿根廷", "Argentina", 60},
		{"AR", "阿根廷", "AR", 40},
	}

	// 批量插入
	for _, r := range rules {
		_, err := s.db.Exec(`INSERT OR IGNORE INTO region_rules
			(region_code, region_name, pattern, priority) VALUES (?, ?, ?, ?)`,
			r.RegionCode, r.RegionName, r.Pattern, r.Priority)
		if err != nil {
			return fmt.Errorf("insert region rule %s: %w", r.Pattern, err)
		}
	}

	return nil
}

// RegionRecognizer 地区识别器
type RegionRecognizer struct {
	rules []RegionRule
}

// NewRegionRecognizer 创建识别器并加载规则（按优先级倒序）
func (s *Store) NewRegionRecognizer() (*RegionRecognizer, error) {
	rows, err := s.db.Query(`
		SELECT region_code, region_name, pattern, priority
		FROM region_rules
		ORDER BY priority DESC, LENGTH(pattern) DESC`)
	if err != nil {
		return nil, fmt.Errorf("load region rules: %w", err)
	}
	defer rows.Close()

	var rules []RegionRule
	for rows.Next() {
		var r RegionRule
		if err := rows.Scan(&r.RegionCode, &r.RegionName, &r.Pattern, &r.Priority); err != nil {
			return nil, fmt.Errorf("scan region rule: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &RegionRecognizer{rules: rules}, nil
}

// Recognize 从节点名识别地区代码
// 按优先级倒序匹配，返回第一个命中的 region_code（子串匹配，大小写不敏感）
// 未匹配返回 "Unknown"
func (r *RegionRecognizer) Recognize(nodeName string) string {
	lowerName := strings.ToLower(nodeName)
	for _, rule := range r.rules {
		if strings.Contains(lowerName, strings.ToLower(rule.Pattern)) {
			return rule.RegionCode
		}
	}
	return "Unknown"
}

// ListRegions 列出所有可用地区（去重）
func (s *Store) ListRegions() ([]struct{ Code, Name string }, error) {
	rows, err := s.db.Query(`
		SELECT DISTINCT region_code, region_name
		FROM region_rules
		ORDER BY region_code`)
	if err != nil {
		return nil, fmt.Errorf("list regions: %w", err)
	}
	defer rows.Close()

	var regions []struct{ Code, Name string }
	for rows.Next() {
		var r struct{ Code, Name string }
		if err := rows.Scan(&r.Code, &r.Name); err != nil {
			return nil, err
		}
		regions = append(regions, r)
	}
	return regions, rows.Err()
}

// RegionInfoMap 返回 地区代码 → 展示信息(中文名 + 国旗 emoji) 的映射,
// 供订阅生成时节点名称标准化取值(见 ADR 0012)。emoji 由地区代码计算。
func (s *Store) RegionInfoMap() (map[string]subscription.RegionInfo, error) {
	regions, err := s.ListRegions()
	if err != nil {
		return nil, err
	}
	m := make(map[string]subscription.RegionInfo, len(regions))
	for _, r := range regions {
		m[r.Code] = subscription.RegionInfo{
			Code:  r.Code,
			Name:  r.Name,
			Emoji: subscription.RegionEmoji(r.Code),
		}
	}
	return m, nil
}
