package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testFingerprintKey 测试用认证密钥(恰好 32 字节)。
var testFingerprintKey = []byte("0123456789abcdef0123456789abcdef")

// populateFingerprintState 向库中写入覆盖全部集合的留存数据。
func populateFingerprintState(t *testing.T, s *Store) {
	t.Helper()

	if _, err := s.CreateEndpoint("手机"); err != nil {
		t.Fatalf("CreateEndpoint() error = %v", err)
	}
	if _, err := s.CreateEndpoint("电脑"); err != nil {
		t.Fatalf("CreateEndpoint() error = %v", err)
	}
	if _, err := s.CreateAirport("机场A", "https://a.example.com/sub?token=aaa"); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	if err := s.CreateSelfHostedNode(&SelfHostedNode{
		Name: "自建1", Protocol: "vless", Server: "1.2.3.4", Port: 443,
		UUID: "uuid-1", Password: "pw-1", TLS: true, Enabled: true,
	}); err != nil {
		t.Fatalf("CreateSelfHostedNode() error = %v", err)
	}
	if err := s.SetSetting("min_available_nodes", "5"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	if err := s.SetSetting("admin_pass_hash", "hash-x"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	if err := s.SetSetting("feishu_webhook", "https://open.feishu.cn/hook/aaa"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	if _, err := s.CreateDistributionPath(&DistributionPath{
		Name: "默认", Path: "def", UpstreamNodeKeys: []string{"s:1"},
		LBStrategy: "random", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateDistributionPath() error = %v", err)
	}
	if err := s.CreateDistributionNode(&DistributionNode{
		Name: "节点1", DistributionPath: "d1", UpstreamNodeKeys: []string{"s:1"},
		LBStrategy: "random", Enabled: true,
	}); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}
}

func fingerprintOf(t *testing.T, s *Store) RetainedStateFingerprint {
	t.Helper()
	fp, err := s.RetainedStateFingerprint(testFingerprintKey)
	if err != nil {
		t.Fatalf("RetainedStateFingerprint() error = %v", err)
	}
	return fp
}

// collectionByName 按名字取集合指纹,不存在则失败。
func collectionByName(t *testing.T, fp RetainedStateFingerprint, name string) CollectionFingerprint {
	t.Helper()
	for _, c := range fp.Collections {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("collection %q not found", name)
	return CollectionFingerprint{}
}

func TestRetainedStateFingerprint_EmptyStore(t *testing.T) {
	s := newTestStore(t)

	fp := fingerprintOf(t, s)

	if fp.Version != FingerprintVersion {
		t.Errorf("Version = %d, want %d", fp.Version, FingerprintVersion)
	}
	if len(fp.SchemaHash) != 64 {
		t.Errorf("len(SchemaHash) = %d, want 64 hex chars", len(fp.SchemaHash))
	}
	if len(fp.Collections) != len(coreTables) {
		t.Fatalf("len(Collections) = %d, want %d", len(fp.Collections), len(coreTables))
	}
	for i, name := range coreTables {
		c := fp.Collections[i]
		if c.Name != name {
			t.Errorf("Collections[%d].Name = %q, want %q", i, c.Name, name)
		}
		if c.Count != 0 {
			t.Errorf("%s.Count = %d, want 0", c.Name, c.Count)
		}
		if len(c.Records) != 0 {
			t.Errorf("len(%s.Records) = %d, want 0", c.Name, len(c.Records))
		}
		if len(c.Hash) != 64 {
			t.Errorf("len(%s.Hash) = %d, want 64 hex chars", c.Name, len(c.Hash))
		}
	}
}

func TestRetainedStateFingerprint_Deterministic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "fp.db")
	s1, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	populateFingerprintState(t, s1)

	fp1 := fingerprintOf(t, s1)
	fp2 := fingerprintOf(t, s1)
	if !reflect.DeepEqual(fp1, fp2) {
		t.Fatal("两次计算结果不一致(同库同密钥)")
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// 重新打开同一数据库文件,结果必须一致(不依赖进程内状态)
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer s2.Close()
	fp3 := fingerprintOf(t, s2)
	if !reflect.DeepEqual(fp1, fp3) {
		t.Fatal("重开数据库后指纹不一致")
	}

	// 记录数符合预期
	want := map[string]int{
		"settings": 3, "endpoints": 2, "airports": 1,
		"self_hosted_nodes": 1, "distribution_paths": 1, "distribution_nodes": 1,
	}
	for name, n := range want {
		if c := collectionByName(t, fp1, name); c.Count != n {
			t.Errorf("%s.Count = %d, want %d", name, c.Count, n)
		}
	}
}

func TestRetainedStateFingerprint_RequiresOpenStoreAndLongKey(t *testing.T) {
	var nilStore *Store
	if _, err := nilStore.RetainedStateFingerprint(testFingerprintKey); err == nil {
		t.Error("nil store expected error, got nil")
	}

	unopened := &Store{}
	if _, err := unopened.RetainedStateFingerprint(testFingerprintKey); err == nil {
		t.Error("unopened store expected error, got nil")
	}

	s := newTestStore(t)
	if _, err := s.RetainedStateFingerprint(testFingerprintKey[:31]); err == nil {
		t.Error("31-byte key expected error, got nil")
	}
	if _, err := s.RetainedStateFingerprint(nil); err == nil {
		t.Error("nil key expected error, got nil")
	}
	if _, err := s.RetainedStateFingerprint(testFingerprintKey); err != nil {
		t.Errorf("32-byte key error = %v, want nil", err)
	}
}

func TestRetainedStateFingerprint_AdditiveColumnKeepsSchemaHash(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)
	before := fingerprintOf(t, s)

	// 增量迁移: 加列 + 加新表,schema 散列与记录指纹都必须不变
	if _, err := s.db.Exec(`ALTER TABLE endpoints ADD COLUMN note TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("ALTER TABLE ADD COLUMN error = %v", err)
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS some_future_table (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("CREATE TABLE error = %v", err)
	}

	after := fingerprintOf(t, s)
	if after.SchemaHash != before.SchemaHash {
		t.Error("加列/加表后 SchemaHash 变化,应当容忍增量迁移")
	}
	if err := after.Preserves(before); err != nil {
		t.Errorf("Preserves() error = %v, want nil", err)
	}
}

func TestRetainedStateFingerprint_RenamedTableChangesSchemaHash(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)
	before := fingerprintOf(t, s)

	if _, err := s.db.Exec(`ALTER TABLE airports RENAME TO airports_legacy`); err != nil {
		t.Fatalf("ALTER TABLE RENAME error = %v", err)
	}

	after := fingerprintOf(t, s)
	if after.SchemaHash == before.SchemaHash {
		t.Error("表被改名后 SchemaHash 未变化,应当检出")
	}

	err := after.Preserves(before)
	if err == nil {
		t.Fatal("Preserves() = nil, want error")
	}
	var pe *PreservationError
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *PreservationError", err)
	}
	if !pe.SchemaChanged {
		t.Error("SchemaChanged = false, want true")
	}
	// airports 表整体缺失 → 其中记录全部 missing
	foundMissing := false
	for _, p := range pe.Problems {
		if p.Collection == "airports" && p.Kind == ProblemMissing {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("缺少 airports missing 问题报告: %v", pe.Problems)
	}
}

func TestPreserves_AdditiveRowsAllowed(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)
	before := fingerprintOf(t, s)

	// 升级后新增行: 合法,不算丢失
	if _, err := s.CreateEndpoint("新设备"); err != nil {
		t.Fatalf("CreateEndpoint() error = %v", err)
	}
	if _, err := s.CreateAirport("机场B", "https://b.example.com/sub?token=bbb"); err != nil {
		t.Fatalf("CreateAirport() error = %v", err)
	}
	after := fingerprintOf(t, s)

	if err := after.Preserves(before); err != nil {
		t.Errorf("after.Preserves(before) error = %v, want nil(新增行应被容忍)", err)
	}

	// 反向比对必须检出: before 里并没有 after 的新增记录
	if err := before.Preserves(after); err == nil {
		t.Error("before.Preserves(after) = nil, want error(反向存在缺失记录)")
	}
}

func TestPreserves_MissingRecord(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)
	before := fingerprintOf(t, s)

	eps, err := s.ListEndpoints()
	if err != nil {
		t.Fatalf("ListEndpoints() error = %v", err)
	}
	if err := s.DeleteEndpoint(eps[0].ID); err != nil {
		t.Fatalf("DeleteEndpoint() error = %v", err)
	}
	after := fingerprintOf(t, s)

	err = after.Preserves(before)
	if err == nil {
		t.Fatal("Preserves() = nil, want error")
	}
	var pe *PreservationError
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *PreservationError", err)
	}
	if len(pe.Problems) != 1 {
		t.Fatalf("len(Problems) = %d, want 1: %v", len(pe.Problems), pe.Problems)
	}
	p := pe.Problems[0]
	if p.Collection != "endpoints" || p.Kind != ProblemMissing {
		t.Errorf("problem = %+v, want endpoints missing", p)
	}
	if p.RecordID == "" {
		t.Error("RecordID 为空,应携带记录 IDHash")
	}
}

func TestPreserves_ChangedRecord(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)
	before := fingerprintOf(t, s)

	eps, err := s.ListEndpoints()
	if err != nil {
		t.Fatalf("ListEndpoints() error = %v", err)
	}
	if err := s.UpdateEndpointAlias(eps[0].ID, "改名后的手机"); err != nil {
		t.Fatalf("UpdateEndpointAlias() error = %v", err)
	}
	after := fingerprintOf(t, s)

	err = after.Preserves(before)
	if err == nil {
		t.Fatal("Preserves() = nil, want error")
	}
	var pe *PreservationError
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *PreservationError", err)
	}
	if len(pe.Problems) != 1 {
		t.Fatalf("len(Problems) = %d, want 1: %v", len(pe.Problems), pe.Problems)
	}
	p := pe.Problems[0]
	if p.Collection != "endpoints" || p.Kind != ProblemChanged {
		t.Errorf("problem = %+v, want endpoints changed", p)
	}
}

func TestPreserves_VersionMismatch(t *testing.T) {
	s := newTestStore(t)
	fp := fingerprintOf(t, s)

	other := fp
	other.Version = FingerprintVersion + 1
	if err := other.Preserves(fp); err == nil {
		t.Error("版本不一致 expected error, got nil")
	}
}

// digestsByID 提取集合中 IDHash → Digest 的映射,便于逐记录断言。
func digestsByID(t *testing.T, fp RetainedStateFingerprint, collection string) map[string]string {
	t.Helper()
	c := collectionByName(t, fp, collection)
	m := make(map[string]string, len(c.Records))
	for _, r := range c.Records {
		m[r.IDHash] = r.Digest
	}
	return m
}

// TestRetainedStateFingerprint_SecretsNotInDigest 验证机密字段绝不进入摘要:
// 只改机密(token/uuid/password/url/机密设置值)指纹必须完全不变;
// 改稳定业务字段则对应记录摘要必须变化。
func TestRetainedStateFingerprint_SecretsNotInDigest(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)
	before := fingerprintOf(t, s)

	// 1) 轮换全部机密值: endpoints.token、airports.url、
	//    self_hosted_nodes.uuid/password、admin_pass_hash、feishu_webhook
	stmts := []string{
		`UPDATE endpoints SET token = 'rotated-secret-token'`,
		`UPDATE airports SET url = 'https://a.example.com/sub?token=rotated'`,
		`UPDATE self_hosted_nodes SET uuid = 'rotated-uuid', password = 'rotated-pw'`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("exec %q error = %v", q, err)
		}
	}
	if err := s.SetSetting("admin_pass_hash", "hash-y"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	if err := s.SetSetting("feishu_webhook", "https://oapi.dingtalk.com/robot/send?access_token=zzz"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}

	afterSecrets := fingerprintOf(t, s)
	if !reflect.DeepEqual(before, afterSecrets) {
		t.Fatal("仅变更机密字段后指纹发生变化,说明机密值进入了摘要")
	}
	if err := afterSecrets.Preserves(before); err != nil {
		t.Errorf("Preserves() error = %v, want nil", err)
	}

	// 2) 改稳定字段: 对应记录摘要必须变化
	stables := []struct {
		collection string
		query      string
	}{
		{"endpoints", `UPDATE endpoints SET alias = 'stable-changed'`},
		{"airports", `UPDATE airports SET name = '机场A-新'`},
		{"self_hosted_nodes", `UPDATE self_hosted_nodes SET server = '9.9.9.9'`},
		{"distribution_paths", `UPDATE distribution_paths SET lb_strategy = 'hash'`},
		{"distribution_nodes", `UPDATE distribution_nodes SET region = 'JP'`},
	}
	for _, tc := range stables {
		prev := fingerprintOf(t, s)
		if _, err := s.db.Exec(tc.query); err != nil {
			t.Fatalf("exec %q error = %v", tc.query, err)
		}
		cur := fingerprintOf(t, s)
		beforeDigests := digestsByID(t, prev, tc.collection)
		afterDigests := digestsByID(t, cur, tc.collection)
		if reflect.DeepEqual(beforeDigests, afterDigests) {
			t.Errorf("%s: 稳定字段变更后记录摘要未变化", tc.collection)
		}
		if err := cur.Preserves(prev); err == nil {
			t.Errorf("%s: 稳定字段变更后 Preserves() = nil, want error", tc.collection)
		}
	}

	// 3) 非机密设置值变化 → settings 摘要变化
	prev := fingerprintOf(t, s)
	if err := s.SetSetting("min_available_nodes", "9"); err != nil {
		t.Fatalf("SetSetting() error = %v", err)
	}
	cur := fingerprintOf(t, s)
	if reflect.DeepEqual(digestsByID(t, prev, "settings"), digestsByID(t, cur, "settings")) {
		t.Error("非机密设置值变更后 settings 摘要未变化")
	}
}

func TestRetainedStateFingerprint_Lines(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)
	fp := fingerprintOf(t, s)

	lines := fp.Lines()
	if len(lines) == 0 {
		t.Fatal("Lines() 为空")
	}
	if lines[0] != "fingerprint_version=2" {
		t.Errorf("lines[0] = %q, want fingerprint_version=2", lines[0])
	}
	if !strings.HasPrefix(lines[1], "schema_hash=") || len(strings.TrimPrefix(lines[1], "schema_hash=")) != 64 {
		t.Errorf("lines[1] = %q, want schema_hash=<64 hex>", lines[1])
	}

	// 每个集合都有 count/hash 行;每条记录一行 <collection>_record_<idhash>=<digest>
	var sawSettingsCount, sawRecordLine bool
	for _, line := range lines {
		if line == "settings_count=3" {
			sawSettingsCount = true
		}
		if strings.HasPrefix(line, "endpoints_record_") {
			sawRecordLine = true
			kv := strings.SplitN(line, "=", 2)
			if len(kv) != 2 || len(kv[1]) != 64 {
				t.Errorf("记录行格式错误: %q", line)
			}
		}
	}
	if !sawSettingsCount {
		t.Error("缺少 settings_count=3 行")
	}
	if !sawRecordLine {
		t.Error("缺少 endpoints_record_ 行")
	}

	// 行输出确定性
	if !reflect.DeepEqual(lines, fp.Lines()) {
		t.Error("Lines() 两次调用结果不一致")
	}
}

func TestRetainedStateFingerprint_DifferentKeysProduceDifferentDigests(t *testing.T) {
	s := newTestStore(t)
	populateFingerprintState(t, s)

	fp1 := fingerprintOf(t, s)
	otherKey := []byte("abcdef0123456789abcdef0123456789")
	fp2, err := s.RetainedStateFingerprint(otherKey)
	if err != nil {
		t.Fatalf("RetainedStateFingerprint() error = %v", err)
	}

	// HMAC 认证属性: 不同密钥 → 记录摘要不同;schema 散列与密钥无关 → 相同
	if fp1.SchemaHash != fp2.SchemaHash {
		t.Error("SchemaHash 不应依赖认证密钥")
	}
	if reflect.DeepEqual(digestsByID(t, fp1, "endpoints"), digestsByID(t, fp2, "endpoints")) {
		t.Error("不同密钥产生了相同记录摘要")
	}
}
