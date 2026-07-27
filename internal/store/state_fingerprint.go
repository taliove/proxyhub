package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// FingerprintVersion 留存数据指纹格式版本。
// 版本历史: 2 = 首个落地版本(production-release ticket 03)。
const FingerprintVersion = 2

// minAuthenticationKeyLen 认证密钥最小长度(字节)。HMAC 密钥过短会失去防伪意义。
const minAuthenticationKeyLen = 32

// coreTables 留存数据涉及的核心表(固定顺序)。
// schema 散列只覆盖这些表的“存在性”: 缺表/表被改名会改变散列,
// 而新增列、新增表等增量迁移不影响散列(见 schemaHash)。
var coreTables = []string{
	"settings",
	"endpoints",
	"airports",
	"self_hosted_nodes",
	"distribution_paths",
	"distribution_nodes",
}

// RecordFingerprint 单条记录的指纹。
// IDHash 与 Digest 都是 HMAC-SHA256(认证密钥),不含密钥无法反推任何原始内容。
type RecordFingerprint struct {
	IDHash string // 记录自然身份(如 endpoints.path)的 HMAC,用于前后比对时配对
	Digest string // 记录稳定业务字段的 HMAC,字段被改写则变化
}

// CollectionFingerprint 一个集合(表)的指纹。
type CollectionFingerprint struct {
	Name    string
	Count   int
	Hash    string              // 集合内全部记录(idhash+digest,按 idhash 排序)的 HMAC
	Records []RecordFingerprint // 按 IDHash 升序,保证输出稳定
}

// RetainedStateFingerprint 留存数据指纹: 证明升级没有静默丢失/改写保留数据。
// 指纹只含 HMAC 摘要与计数,不泄露任何字段明文;机密字段(凭证/口令)从不进入摘要。
type RetainedStateFingerprint struct {
	Version     int
	SchemaHash  string // 核心表存在性散列(纯 SHA-256,与认证密钥无关)
	Collections []CollectionFingerprint
}

// RetainedStateFingerprint 计算当前库中留存数据的 HMAC 认证指纹。
// authenticationKey 至少 32 字节;升级前后使用同一密钥计算两次,再用 Preserves 比对。
// store 未打开或密钥过短时返回错误。
func (s *Store) RetainedStateFingerprint(authenticationKey []byte) (RetainedStateFingerprint, error) {
	if s == nil || s.db == nil {
		return RetainedStateFingerprint{}, errors.New("store: 数据库未打开")
	}
	if len(authenticationKey) < minAuthenticationKeyLen {
		return RetainedStateFingerprint{}, fmt.Errorf(
			"store: 认证密钥仅 %d 字节,至少需要 %d 字节", len(authenticationKey), minAuthenticationKeyLen)
	}

	present, err := s.coreTablesPresent()
	if err != nil {
		return RetainedStateFingerprint{}, err
	}

	fp := RetainedStateFingerprint{
		Version:    FingerprintVersion,
		SchemaHash: schemaHash(present),
	}

	// 各集合的记录加载器(固定顺序)。机密列不被 SELECT,根本不离开数据库。
	loaders := []struct {
		name string
		load func(db *sql.DB, key []byte) ([]RecordFingerprint, error)
	}{
		{"settings", loadSettingRecords},
		{"endpoints", loadEndpointRecords},
		{"airports", loadAirportRecords},
		{"self_hosted_nodes", loadSelfHostedNodeRecords},
		{"distribution_paths", loadDistributionPathRecords},
		{"distribution_nodes", loadDistributionNodeRecords},
	}

	for _, l := range loaders {
		var records []RecordFingerprint
		if present[l.name] {
			records, err = l.load(s.db, authenticationKey)
			if err != nil {
				return RetainedStateFingerprint{}, fmt.Errorf("store: 计算 %s 指纹: %w", l.name, err)
			}
		}
		sort.Slice(records, func(i, j int) bool { return records[i].IDHash < records[j].IDHash })
		fp.Collections = append(fp.Collections, CollectionFingerprint{
			Name:    l.name,
			Count:   len(records),
			Hash:    collectionHash(authenticationKey, l.name, records),
			Records: records,
		})
	}
	return fp, nil
}

// coreTablesPresent 查询库中实际存在的表名集合。
func (s *Store) coreTablesPresent() (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'`)
	if err != nil {
		return nil, fmt.Errorf("store: 查询表清单: %w", err)
	}
	defer rows.Close()

	present := make(map[string]bool, len(coreTables))
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("store: 扫描表名: %w", err)
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: 读取表清单: %w", err)
	}
	return present, nil
}

// schemaHash 核心 schema 散列: 只编码核心表是否存在。
// 缺表/表改名 → 散列变化;增量加列/加表 → 散列不变(容忍增量迁移)。
// 用纯 SHA-256 而非 HMAC,使 schema 散列与认证密钥无关,可跨不同密钥比对。
func schemaHash(present map[string]bool) string {
	var b strings.Builder
	for _, t := range coreTables {
		b.WriteString(t)
		if present[t] {
			b.WriteString("=1;")
		} else {
			b.WriteString("=0;")
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// hmacHex 计算 HMAC-SHA256 并返回十六进制。各输入段做长度前缀编码,避免拼接歧义。
func hmacHex(key []byte, parts ...string) string {
	mac := hmac.New(sha256.New, key)
	for _, p := range parts {
		mac.Write([]byte(strconv.Itoa(len(p))))
		mac.Write([]byte{':'})
		mac.Write([]byte(p))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

// recordIDHash 记录身份散列: 只依赖集合名与自然身份,字段变化不影响(用于前后配对)。
func recordIDHash(key []byte, collection, identity string) string {
	return hmacHex(key, "id", collection, identity)
}

// recordDigest 记录摘要: 覆盖身份 + 稳定业务字段(fields 为 "name=value" 形式)。
func recordDigest(key []byte, collection, identity string, fields ...string) string {
	parts := make([]string, 0, len(fields)+3)
	parts = append(parts, "rec", collection, identity)
	parts = append(parts, fields...)
	return hmacHex(key, parts...)
}

// collectionHash 集合级散列。records 必须已按 IDHash 排序。
func collectionHash(key []byte, collection string, records []RecordFingerprint) string {
	parts := make([]string, 0, 2*len(records)+2)
	parts = append(parts, "col", collection)
	for _, r := range records {
		parts = append(parts, r.IDHash, r.Digest)
	}
	return hmacHex(key, parts...)
}

// secretSettingSubstrings 设置键中的机密特征(小写子串匹配)。
// 命中的设置只记录“存在性”,值绝不进入摘要。
var secretSettingSubstrings = []string{
	"pass",    // admin_pass_hash、password、passwd 等
	"secret",  // 各类显式机密
	"token",   // 访问令牌
	"webhook", // feishu_webhook 等 URL 内嵌凭证
	"apikey",  // API 密钥
	"api_key", // API 密钥(下划线风格)
	"private", // 私钥材料
}

// isSecretSettingKey 判断设置键是否承载机密值。
func isSecretSettingKey(key string) bool {
	lower := strings.ToLower(key)
	for _, pat := range secretSettingSubstrings {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}

// loadSettingRecords settings: 身份=key;非机密设置摘要覆盖 value,机密设置只覆盖存在性。
// 读 system_settings(ticket 06 settings 拆分后的全局作用域;遗留 settings 表
// 仅作回滚备份,不再参与指纹)。
func loadSettingRecords(db *sql.DB, key []byte) ([]RecordFingerprint, error) {
	rows, err := db.Query(`SELECT key, value FROM system_settings`)
	if err != nil {
		return nil, fmt.Errorf("查询 settings: %w", err)
	}
	defer rows.Close()

	var records []RecordFingerprint
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("扫描 settings: %w", err)
		}
		// 机密设置(admin_pass_hash、feishu_webhook 等)的值绝不进入摘要
		field := "value=" + v
		if isSecretSettingKey(k) {
			field = "secret=1"
		}
		records = append(records, RecordFingerprint{
			IDHash: recordIDHash(key, "settings", k),
			Digest: recordDigest(key, "settings", k, field),
		})
	}
	return records, rows.Err()
}

// loadEndpointRecords endpoints: 身份=path;覆盖 alias/enabled/name_mode/name_template/conditions。
// token 是订阅凭证(机密),不读取、不进入摘要。
func loadEndpointRecords(db *sql.DB, key []byte) ([]RecordFingerprint, error) {
	rows, err := db.Query(`SELECT path, alias, enabled, name_mode, name_template, conditions FROM endpoints`)
	if err != nil {
		return nil, fmt.Errorf("查询 endpoints: %w", err)
	}
	defer rows.Close()

	var records []RecordFingerprint
	for rows.Next() {
		var path, alias, nameMode, nameTemplate, conditions string
		var enabled int
		if err := rows.Scan(&path, &alias, &enabled, &nameMode, &nameTemplate, &conditions); err != nil {
			return nil, fmt.Errorf("扫描 endpoints: %w", err)
		}
		records = append(records, RecordFingerprint{
			IDHash: recordIDHash(key, "endpoints", path),
			Digest: recordDigest(key, "endpoints", path,
				"alias="+alias,
				"enabled="+strconv.Itoa(enabled),
				"name_mode="+nameMode,
				"name_template="+nameTemplate,
				"conditions="+conditions),
		})
	}
	return records, rows.Err()
}

// loadAirportRecords airports: 身份=id(无唯一业务键);覆盖 name/abbr/enabled。
// url 内嵌订阅凭证(机密),不读取、不进入摘要。
func loadAirportRecords(db *sql.DB, key []byte) ([]RecordFingerprint, error) {
	rows, err := db.Query(`SELECT id, name, abbr, enabled FROM airports`)
	if err != nil {
		return nil, fmt.Errorf("查询 airports: %w", err)
	}
	defer rows.Close()

	var records []RecordFingerprint
	for rows.Next() {
		var id int64
		var name, abbr string
		var enabled int
		if err := rows.Scan(&id, &name, &abbr, &enabled); err != nil {
			return nil, fmt.Errorf("扫描 airports: %w", err)
		}
		identity := strconv.FormatInt(id, 10)
		records = append(records, RecordFingerprint{
			IDHash: recordIDHash(key, "airports", identity),
			Digest: recordDigest(key, "airports", identity,
				"name="+name,
				"abbr="+abbr,
				"enabled="+strconv.Itoa(enabled)),
		})
	}
	return records, rows.Err()
}

// loadSelfHostedNodeRecords self_hosted_nodes: 身份=id;覆盖协议/地址/端口等稳定配置。
// uuid 与 password 是代理凭证(机密),不读取、不进入摘要。
func loadSelfHostedNodeRecords(db *sql.DB, key []byte) ([]RecordFingerprint, error) {
	rows, err := db.Query(`SELECT id, name, protocol, server, port, cipher, alter_id,
		network, tls, region_code, grpc_service_name, enabled FROM self_hosted_nodes`)
	if err != nil {
		return nil, fmt.Errorf("查询 self_hosted_nodes: %w", err)
	}
	defer rows.Close()

	var records []RecordFingerprint
	for rows.Next() {
		var id int64
		var name, protocol, server, cipher, network, regionCode, grpcServiceName string
		var port, alterID, tls, enabled int
		if err := rows.Scan(&id, &name, &protocol, &server, &port, &cipher, &alterID,
			&network, &tls, &regionCode, &grpcServiceName, &enabled); err != nil {
			return nil, fmt.Errorf("扫描 self_hosted_nodes: %w", err)
		}
		identity := strconv.FormatInt(id, 10)
		records = append(records, RecordFingerprint{
			IDHash: recordIDHash(key, "self_hosted_nodes", identity),
			Digest: recordDigest(key, "self_hosted_nodes", identity,
				"name="+name,
				"protocol="+protocol,
				"server="+server,
				"port="+strconv.Itoa(port),
				"cipher="+cipher,
				"alter_id="+strconv.Itoa(alterID),
				"network="+network,
				"tls="+strconv.Itoa(tls),
				"region_code="+regionCode,
				"grpc_service_name="+grpcServiceName,
				"enabled="+strconv.Itoa(enabled)),
		})
	}
	return records, rows.Err()
}

// loadDistributionPathRecords distribution_paths: 身份=path;覆盖路由定义字段。
// 流量计数器与 last_access 是易变运行态(非机密但不稳定),不进入摘要。
func loadDistributionPathRecords(db *sql.DB, key []byte) ([]RecordFingerprint, error) {
	rows, err := db.Query(`SELECT path, name, upstream_node_keys, lb_strategy, enabled FROM distribution_paths`)
	if err != nil {
		return nil, fmt.Errorf("查询 distribution_paths: %w", err)
	}
	defer rows.Close()

	var records []RecordFingerprint
	for rows.Next() {
		var path, name, upstreamKeys, lbStrategy string
		var enabled int
		if err := rows.Scan(&path, &name, &upstreamKeys, &lbStrategy, &enabled); err != nil {
			return nil, fmt.Errorf("扫描 distribution_paths: %w", err)
		}
		records = append(records, RecordFingerprint{
			IDHash: recordIDHash(key, "distribution_paths", path),
			Digest: recordDigest(key, "distribution_paths", path,
				"name="+name,
				"upstream_node_keys="+upstreamKeys,
				"lb_strategy="+lbStrategy,
				"enabled="+strconv.Itoa(enabled)),
		})
	}
	return records, rows.Err()
}

// loadDistributionNodeRecords distribution_nodes: 身份=distribution_path;覆盖路由定义字段。
// updated_at 等时间戳易变,不进入摘要。
func loadDistributionNodeRecords(db *sql.DB, key []byte) ([]RecordFingerprint, error) {
	rows, err := db.Query(`SELECT distribution_path, name, region, upstream_node_keys, lb_strategy, enabled FROM distribution_nodes`)
	if err != nil {
		return nil, fmt.Errorf("查询 distribution_nodes: %w", err)
	}
	defer rows.Close()

	var records []RecordFingerprint
	for rows.Next() {
		var distPath, name, region, upstreamKeys, lbStrategy string
		var enabled int
		if err := rows.Scan(&distPath, &name, &region, &upstreamKeys, &lbStrategy, &enabled); err != nil {
			return nil, fmt.Errorf("扫描 distribution_nodes: %w", err)
		}
		records = append(records, RecordFingerprint{
			IDHash: recordIDHash(key, "distribution_nodes", distPath),
			Digest: recordDigest(key, "distribution_nodes", distPath,
				"name="+name,
				"region="+region,
				"upstream_node_keys="+upstreamKeys,
				"lb_strategy="+lbStrategy,
				"enabled="+strconv.Itoa(enabled)),
		})
	}
	return records, rows.Err()
}

// PreservationProblemKind 保留数据问题的类别。
type PreservationProblemKind string

const (
	// ProblemMissing before 中的记录在 after 里不存在(升级丢数据)
	ProblemMissing PreservationProblemKind = "missing"
	// ProblemChanged before 中的记录在 after 里稳定字段被改写
	ProblemChanged PreservationProblemKind = "changed"
)

// PreservationProblem 一条记录的保留问题。RecordID 是记录的 IDHash(不含明文身份)。
type PreservationProblem struct {
	Collection string
	RecordID   string
	Kind       PreservationProblemKind
}

// PreservationError 聚合升级前后指纹比对发现的全部问题。
type PreservationError struct {
	SchemaChanged bool                  // 核心 schema 散列不一致(缺表/表被改名)
	Problems      []PreservationProblem // 记录级缺失/变更清单
}

func (e *PreservationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "留存数据指纹比对失败: %d 处记录问题", len(e.Problems))
	if e.SchemaChanged {
		b.WriteString(",核心 schema 已变化(表缺失或被改名)")
	}
	for _, p := range e.Problems {
		fmt.Fprintf(&b, "\n  collection=%s record=%s kind=%s", p.Collection, p.RecordID, p.Kind)
	}
	return b.String()
}

// Preserves 校验 f(升级后指纹)是否完整保留了 before(升级前指纹)中的全部记录:
// before 里每条记录必须在 f 中存在且摘要一致;f 中多出的记录(升级后新增行)视为合法。
// 两者 Version 不一致时返回普通错误;其余差异返回 *PreservationError(可用 errors.As 提取)。
func (f RetainedStateFingerprint) Preserves(before RetainedStateFingerprint) error {
	if f.Version != before.Version {
		return fmt.Errorf("指纹版本不一致: before=%d after=%d,无法比对", before.Version, f.Version)
	}

	afterByName := make(map[string]CollectionFingerprint, len(f.Collections))
	for _, c := range f.Collections {
		afterByName[c.Name] = c
	}

	result := &PreservationError{SchemaChanged: f.SchemaHash != before.SchemaHash}
	for _, bc := range before.Collections {
		ac, ok := afterByName[bc.Name]
		if ok && ac.Hash == bc.Hash {
			continue // 集合散列一致,快速通过
		}
		afterByID := make(map[string]string, len(ac.Records))
		if ok {
			for _, r := range ac.Records {
				afterByID[r.IDHash] = r.Digest
			}
		}
		for _, r := range bc.Records {
			digest, exists := afterByID[r.IDHash]
			switch {
			case !exists:
				result.Problems = append(result.Problems, PreservationProblem{
					Collection: bc.Name, RecordID: r.IDHash, Kind: ProblemMissing})
			case digest != r.Digest:
				result.Problems = append(result.Problems, PreservationProblem{
					Collection: bc.Name, RecordID: r.IDHash, Kind: ProblemChanged})
			}
		}
	}

	if result.SchemaChanged || len(result.Problems) > 0 {
		return result
	}
	return nil
}

// Lines 以稳定的 key=value 行格式导出指纹(机器可解析,供 CLI 打印)。行序固定:
//
//	fingerprint_version=2
//	schema_hash=<64 hex>                      核心表存在性散列(与认证密钥无关)
//	<collection>_count=<n>                    集合按固定顺序: settings, endpoints,
//	<collection>_hash=<64 hex>                airports, self_hosted_nodes,
//	<collection>_record_<idhash>=<64 hex>     distribution_paths, distribution_nodes;
//	                                          记录行按 idhash 升序
func (f RetainedStateFingerprint) Lines() []string {
	total := 2
	for _, c := range f.Collections {
		total += 2 + len(c.Records)
	}
	lines := make([]string, 0, total)
	lines = append(lines, "fingerprint_version="+strconv.Itoa(f.Version))
	lines = append(lines, "schema_hash="+f.SchemaHash)
	for _, c := range f.Collections {
		lines = append(lines, c.Name+"_count="+strconv.Itoa(c.Count))
		lines = append(lines, c.Name+"_hash="+c.Hash)
		for _, r := range c.Records {
			lines = append(lines, c.Name+"_record_"+r.IDHash+"="+r.Digest)
		}
	}
	return lines
}
