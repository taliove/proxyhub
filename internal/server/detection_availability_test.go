package server

import (
	"testing"

	"github.com/taliove/proxyhub/internal/detection"
)

// TestNodeAvailabilityResult 节点可用性只取通用目标结果,不受专用解锁目标影响。
func TestNodeAvailabilityResult(t *testing.T) {
	genericPass := detection.Result{TargetName: "connectivity", Available: true, Latency: 42}
	netflixFail := detection.Result{TargetName: "Netflix", Available: false, Error: "not implemented"}

	t.Run("generic at index 0 (历史常见配置,行为不变)", func(t *testing.T) {
		targets := []detection.Target{
			{Name: "connectivity"}, // 空 kind = generic
			{Name: "Netflix", Kind: detection.KindNetflix},
		}
		results := []detection.Result{genericPass, netflixFail}
		got, ok := nodeAvailabilityResult(targets, results)
		if !ok || !got.Available || got.Latency != 42 {
			t.Fatalf("got=%+v ok=%v, want generic pass result", got, ok)
		}
	})

	t.Run("generic not at index 0", func(t *testing.T) {
		targets := []detection.Target{
			{Name: "Netflix", Kind: detection.KindNetflix},
			{Name: "connectivity", Kind: detection.KindGeneric},
		}
		results := []detection.Result{netflixFail, genericPass}
		got, ok := nodeAvailabilityResult(targets, results)
		if !ok || !got.Available {
			t.Fatalf("got=%+v ok=%v, want generic result at index 1", got, ok)
		}
	})

	t.Run("no generic target (播种的纯解锁目标) 不篡改可用性", func(t *testing.T) {
		targets := []detection.Target{
			{Name: "Netflix", Kind: detection.KindNetflix},
			{Name: "OpenAI", Kind: detection.KindOpenAI},
		}
		results := []detection.Result{netflixFail, {TargetName: "OpenAI", Available: false}}
		if _, ok := nodeAvailabilityResult(targets, results); ok {
			t.Fatal("ok=true for all-specialized targets, want false (must not force node unavailable)")
		}
	})

	t.Run("empty", func(t *testing.T) {
		if _, ok := nodeAvailabilityResult(nil, nil); ok {
			t.Fatal("ok=true for empty, want false")
		}
	})
}
