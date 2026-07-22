package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/taliove/proxyhub/internal/subscription"
)

// TestToNodeViews_ProtocolParamsPassthrough 节点详情透出完整协议参数(见 ticket 0016):
// plugin/plugin_opts 是排障刚需(SS 混淆丢失 = 节点不可用);cipher/alter_id/
// grpc_service_name/insecure 同为已落库且排障有用的字段。uuid/password 属凭证,绝不透出。
func TestToNodeViews_ProtocolParamsPassthrough(t *testing.T) {
	n := &subscription.Node{
		Name: "ss-obfs", Type: "ss", Server: "a.example.com", Port: 8388,
		UUID: "00000000-0000-0000-0000-000000000000", Password: "secret-pw",
		Cipher: "aes-128-gcm", AlterID: 0,
		Plugin: "obfs-local", PluginOpts: "obfs=http;obfs-host=www.example.com",
		Network: "ws", TLS: true, SNI: "sni.example.com", Insecure: true,
		GrpcServiceName: "grpcsvc",
		Source:          "机场A",
	}

	views := toNodeViews([]*subscription.Node{n}, nil, nil, nil, nil)
	if len(views) != 1 {
		t.Fatalf("views len = %d, want 1", len(views))
	}
	v := views[0]
	if v.Plugin != "obfs-local" || v.PluginOpts != "obfs=http;obfs-host=www.example.com" {
		t.Errorf("plugin 透出错误: plugin=%q plugin_opts=%q", v.Plugin, v.PluginOpts)
	}
	if v.Cipher != "aes-128-gcm" {
		t.Errorf("cipher = %q, want aes-128-gcm", v.Cipher)
	}
	if v.GrpcServiceName != "grpcsvc" {
		t.Errorf("grpc_service_name = %q, want grpcsvc", v.GrpcServiceName)
	}
	if !v.Insecure {
		t.Errorf("insecure = false, want true")
	}

	// 序列化输出不得包含凭证字段
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal error = %v", err)
	}
	for _, leaked := range []string{"secret-pw", "00000000-0000-0000-0000-000000000000", `"password"`, `"uuid"`} {
		if strings.Contains(string(data), leaked) {
			t.Errorf("视图泄露敏感字段 %q: %s", leaked, data)
		}
	}
}

// TestToNodeViews_AvailabilitySource 可用性判定来源透出:
// never(从未检测)/ health(仅健康检查)/ real(真实检测),口径由 Node.AvailabilitySource 统一定义。
func TestToNodeViews_AvailabilitySource(t *testing.T) {
	mk := func(server string, kind string) *subscription.Node {
		return &subscription.Node{
			Name: "n", Type: "ss", Server: server, Port: 8388,
			Source: "机场A", DetectionKind: kind,
		}
	}
	nodes := []*subscription.Node{
		mk("a.example.com", ""),
		mk("b.example.com", subscription.DetectionKindHealth),
		mk("c.example.com", subscription.DetectionKindReal),
	}

	views := toNodeViews(nodes, nil, nil, nil, nil)
	want := []string{
		subscription.AvailabilitySourceNever,
		subscription.AvailabilitySourceHealth,
		subscription.AvailabilitySourceReal,
	}
	for i, w := range want {
		if views[i].AvailabilitySource != w {
			t.Errorf("views[%d].AvailabilitySource = %q, want %q", i, views[i].AvailabilitySource, w)
		}
	}
}

// TestToNodeViews_DetectionLastCheck 最近真实检测时间透出:
// 有记录时序列化为 RFC3339;零值(从未检测)省略键,不与"很久之前"混淆。
func TestToNodeViews_DetectionLastCheck(t *testing.T) {
	checked := time.Date(2026, 7, 22, 10, 30, 0, 0, time.UTC)
	withCheck := &subscription.Node{
		Name: "n1", Type: "ss", Server: "a.example.com", Port: 8388,
		Source: "机场A", DetectionLastCheck: checked, DetectionKind: subscription.DetectionKindReal,
	}
	never := &subscription.Node{
		Name: "n2", Type: "ss", Server: "b.example.com", Port: 8388, Source: "机场A",
	}

	views := toNodeViews([]*subscription.Node{withCheck, never}, nil, nil, nil, nil)

	if views[0].DetectionLastCheck == nil || !views[0].DetectionLastCheck.Equal(checked) {
		t.Errorf("有检测记录节点 DetectionLastCheck = %v, want %v", views[0].DetectionLastCheck, checked)
	}
	if views[1].DetectionLastCheck != nil {
		t.Errorf("从未检测节点 DetectionLastCheck = %v, want nil", views[1].DetectionLastCheck)
	}

	data, _ := json.Marshal(views[1])
	if strings.Contains(string(data), "detection_last_check") {
		t.Errorf("零值检测时间应省略 detection_last_check 键, got %s", data)
	}
	data, _ = json.Marshal(views[0])
	if !strings.Contains(string(data), `"detection_last_check":"2026-07-22T10:30:00Z"`) {
		t.Errorf("检测时间应序列化为 RFC3339, got %s", data)
	}
}

// TestToNodeViews_DetectionFailReason 最近检测失败原因透出(ticket 0017):
// 失败节点透出分类与截断详情;成功/从未检测节点为空且序列化省略键。
func TestToNodeViews_DetectionFailReason(t *testing.T) {
	failed := &subscription.Node{
		Name: "n1", Type: "ss", Server: "a.example.com", Port: 8388, Source: "机场A",
		Available:           false,
		DetectionKind:       subscription.DetectionKindReal,
		DetectionFailReason: "handshake",
		DetectionFailDetail: "tls: handshake failure",
	}
	ok := &subscription.Node{
		Name: "n2", Type: "ss", Server: "b.example.com", Port: 8388, Source: "机场A",
		Available:     true,
		DetectionKind: subscription.DetectionKindReal,
	}

	views := toNodeViews([]*subscription.Node{failed, ok}, nil, nil, nil, nil)

	if views[0].DetectionFailReason != "handshake" || views[0].DetectionFailDetail != "tls: handshake failure" {
		t.Errorf("失败原因透出错误: reason=%q detail=%q", views[0].DetectionFailReason, views[0].DetectionFailDetail)
	}
	if views[1].DetectionFailReason != "" || views[1].DetectionFailDetail != "" {
		t.Errorf("成功节点失败原因应为空: reason=%q detail=%q", views[1].DetectionFailReason, views[1].DetectionFailDetail)
	}

	data, _ := json.Marshal(views[1])
	if strings.Contains(string(data), "detection_fail_reason") {
		t.Errorf("空失败原因应省略 detection_fail_reason 键, got %s", data)
	}
}
