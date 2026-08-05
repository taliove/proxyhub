package store

import (
	"testing"

	"github.com/taliove/proxyhub/internal/subscription"
)

// reality 参数快照 roundtrip(ticket 1 / spec #58):节点池保存/读取后
// flow/pbk/sid/fp 不丢,跨刷新保真;非 reality 节点字段为空(零回归)。
// fixture 全合成。
func TestNodePool_RealityParamsRoundTrip(t *testing.T) {
	st := newTestStore(t)

	pool := []*subscription.Node{
		{Name: "IEPL01", Type: "vless", Server: "iepl01.example.com", Port: 20014,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "tcp", TLS: true,
			SNI: "img.example.com", Flow: "xtls-rprx-vision",
			RealityPublicKey:  "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
			RealityShortID:    "d28a3d8c",
			ClientFingerprint: "chrome",
			Region:            "SG", Source: "机场A"},
		{Name: "WS01", Type: "vless", Server: "ws01.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "ws", TLS: true,
			SNI: "wss.example.com", Region: "HK", Source: "机场A"},
	}
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("LoadNodePool() len = %d, want 2", len(got))
	}

	r := got[0]
	if r.Flow != "xtls-rprx-vision" {
		t.Errorf("reality node Flow = %q, want xtls-rprx-vision", r.Flow)
	}
	if r.RealityPublicKey != "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8" {
		t.Errorf("reality node RealityPublicKey = %q, want 合成公钥", r.RealityPublicKey)
	}
	if r.RealityShortID != "d28a3d8c" {
		t.Errorf("reality node RealityShortID = %q, want d28a3d8c", r.RealityShortID)
	}
	if r.ClientFingerprint != "chrome" {
		t.Errorf("reality node ClientFingerprint = %q, want chrome", r.ClientFingerprint)
	}
	if r.SNI != "img.example.com" || !r.TLS {
		t.Errorf("reality node SNI/TLS = %q/%v, want img.example.com/true", r.SNI, r.TLS)
	}

	w := got[1]
	if w.Flow != "" || w.RealityPublicKey != "" || w.RealityShortID != "" || w.ClientFingerprint != "" {
		t.Errorf("非 reality 节点 reality 字段应为空, got flow=%q pbk=%q sid=%q fp=%q",
			w.Flow, w.RealityPublicKey, w.RealityShortID, w.ClientFingerprint)
	}
	if w.SNI != "wss.example.com" {
		t.Errorf("非 reality 节点 SNI = %q, want wss.example.com", w.SNI)
	}

	// 再次保存(下一轮刷新)后字段仍完整:upsert 路径不丢参数
	if err := st.SaveNodePool(pool); err != nil {
		t.Fatalf("SaveNodePool() 第二次 error = %v", err)
	}
	got2, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() 第二次 error = %v", err)
	}
	if got2[0].RealityPublicKey != r.RealityPublicKey || got2[0].Flow != r.Flow {
		t.Errorf("二次保存后 reality 参数丢失: pbk=%q flow=%q", got2[0].RealityPublicKey, got2[0].Flow)
	}
}

// 升级路径(显式决策,见 022 迁移注释):旧版本解析的 vless 行 node_key 不含 SNI,
// 新解析对所有带 sni 的 vless 链接(tls 与 reality 皆然)产出 server:port:sni 新 key。
// 升级后首次刷新,旧 key 行必须被标 stale(而非报错/消失/覆盖新行),
// 新 key 行正常在架——一次性 churn,超期自动清理。
func TestNodePool_VlessSNIKeyUpgradePath(t *testing.T) {
	st := newTestStore(t)

	// 旧版本快照:vless 行无 SNI,key = server:port(一个 reality 节点 + 一个 tls+ws 节点)
	oldPool := []*subscription.Node{
		{Name: "IEPL01", Type: "vless", Server: "iepl01.example.com", Port: 20014,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "tcp", TLS: true,
			Region: "SG", Source: "机场A", Available: true, Latency: 88},
		{Name: "WS01", Type: "vless", Server: "ws01.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "ws", TLS: true,
			Region: "HK", Source: "机场A", Available: true, Latency: 66},
	}
	if err := st.SaveNodePool(oldPool); err != nil {
		t.Fatalf("SaveNodePool(旧池) error = %v", err)
	}

	// 升级后首次刷新:同一批节点新解析带 SNI,reality 节点另有完整 reality 参数
	newPool := []*subscription.Node{
		{Name: "IEPL01", Type: "vless", Server: "iepl01.example.com", Port: 20014,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "tcp", TLS: true,
			SNI: "img.example.com", Flow: "xtls-rprx-vision",
			RealityPublicKey:  "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8",
			RealityShortID:    "d28a3d8c",
			ClientFingerprint: "chrome",
			Region: "SG", Source: "机场A"},
		{Name: "WS01", Type: "vless", Server: "ws01.example.com", Port: 443,
			UUID: "00000000-0000-0000-0000-000000000000", Network: "ws", TLS: true,
			SNI: "wss.example.com", Region: "HK", Source: "机场A"},
	}
	if err := st.SaveNodePool(newPool); err != nil {
		t.Fatalf("SaveNodePool(新池) error = %v", err)
	}

	got, err := st.LoadNodePool()
	if err != nil {
		t.Fatalf("LoadNodePool() error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("LoadNodePool() len = %d, want 4(2 旧 key stale 行 + 2 新 key 在架行)", len(got))
	}

	staleCount := 0
	activeBySNI := map[string]*subscription.Node{}
	for _, n := range got {
		if n.Stale {
			staleCount++
			if n.SNI != "" {
				t.Errorf("stale 行应为旧 key(无 SNI), got SNI=%q", n.SNI)
			}
			continue
		}
		activeBySNI[n.SNI] = n
	}
	if staleCount != 2 {
		t.Errorf("stale 行数 = %d, want 2(reality + tls 两类旧 key 行都应标 stale)", staleCount)
	}
	if len(activeBySNI) != 2 {
		t.Fatalf("在架行数 = %d, want 2", len(activeBySNI))
	}
	if activeBySNI["img.example.com"].RealityPublicKey == "" {
		t.Error("reality 在架行 reality 参数不完整")
	}
	if activeBySNI["wss.example.com"].RealityPublicKey != "" || activeBySNI["wss.example.com"].Flow != "" {
		t.Error("tls 在架行不应携带 reality 参数")
	}
}
