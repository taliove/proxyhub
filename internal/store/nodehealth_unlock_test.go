package store

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// TestNodeHealthUnlockColumnsMigrated 证明 011 迁移真正接入迁移链路并执行了
// (node_health 表在裸库 Open 后即含 level/region 列)。
func TestNodeHealthUnlockColumnsMigrated(t *testing.T) {
	st := newTestStore(t)
	cols := map[string]bool{}
	rows, err := st.db.Query(`SELECT name FROM pragma_table_info('node_health')`)
	if err != nil {
		t.Fatalf("inspect node_health columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	for _, want := range []string{"level", "region"} {
		if !cols[want] {
			t.Errorf("node_health missing column %q after migration", want)
		}
	}
}

// TestSaveDetectionResults_LevelRegionRoundtrip 专用解锁结果的 Level/Region
// 落库后能被 GetLatestDetectionResults 原样读回;generic 结果两字段留空。
func TestSaveDetectionResults_LevelRegionRoundtrip(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"
	results := []detection.Result{
		{NodeKey: key, TargetName: "Netflix", Available: true, Latency: 88, Level: detection.LevelFull, Region: "US"},
		{NodeKey: key, TargetName: "connectivity", Available: true, Latency: 30}, // generic: Level/Region 空
	}
	if err := st.SaveDetectionResults(results, "TestNode", "TestAirport"); err != nil {
		t.Fatalf("SaveDetectionResults: %v", err)
	}

	got, err := st.GetLatestDetectionResults([]string{key})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	views := got[key]
	var nf, gen *DetectionResultView
	for i := range views {
		switch views[i].TargetName {
		case "Netflix":
			nf = &views[i]
		case "connectivity":
			gen = &views[i]
		}
	}
	if nf == nil || gen == nil {
		t.Fatalf("missing views: netflix=%v generic=%v", nf, gen)
	}
	if nf.Level != detection.LevelFull || nf.Region != "US" {
		t.Errorf("netflix level/region = %q/%q, want full/US", nf.Level, nf.Region)
	}
	if gen.Level != "" || gen.Region != "" {
		t.Errorf("generic level/region = %q/%q, want empty", gen.Level, gen.Region)
	}
}

// TestDetectionResultView_OmitemptyContract generic 结果(Level/Region 空)
// 序列化后 JSON 不出现 level/region 两键;专用结果则出现。
func TestDetectionResultView_OmitemptyContract(t *testing.T) {
	generic := DetectionResultView{TargetName: "connectivity", Available: true, Latency: 30}
	b, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("marshal generic: %v", err)
	}
	if s := string(b); strings.Contains(s, `"level"`) || strings.Contains(s, `"region"`) {
		t.Errorf("generic JSON leaks level/region: %s", s)
	}

	full := DetectionResultView{TargetName: "Netflix", Available: true, Latency: 88, Level: detection.LevelFull, Region: "US"}
	b, err = json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full: %v", err)
	}
	if s := string(b); !strings.Contains(s, `"level":"full"`) || !strings.Contains(s, `"region":"US"`) {
		t.Errorf("full JSON missing level/region: %s", s)
	}
}

// TestSaveDetectionResults_ErrorResultNoLevel 检测失败/未知 kind/未实现的错误结果
// 不得产生 level:落库列为空,读回 JSON 无 level 键。
func TestSaveDetectionResults_ErrorResultNoLevel(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"
	// 错误结果:detection 层对失败/未实现/未知 kind 只填 Error,Level/Region 恒空。
	results := []detection.Result{
		{NodeKey: key, TargetName: "Disney+", Available: false, Error: "not implemented"},
	}
	if err := st.SaveDetectionResults(results, "TestNode", "TestAirport"); err != nil {
		t.Fatalf("SaveDetectionResults: %v", err)
	}

	// 直接查列值,断言空串落库。
	var level, region string
	err := st.db.QueryRow(
		`SELECT level, region FROM node_health WHERE node_key = ? AND target_name = 'Disney+' ORDER BY id DESC LIMIT 1`, key).
		Scan(&level, &region)
	if err != nil {
		t.Fatalf("read level/region: %v", err)
	}
	if level != "" || region != "" {
		t.Errorf("error result stored level/region = %q/%q, want empty", level, region)
	}

	// 读回视图序列化无 level 键。
	got, err := st.GetLatestDetectionResults([]string{key})
	if err != nil {
		t.Fatalf("GetLatestDetectionResults: %v", err)
	}
	b, _ := json.Marshal(got[key])
	if strings.Contains(string(b), `"level"`) {
		t.Errorf("error result JSON leaks level: %s", b)
	}
}
