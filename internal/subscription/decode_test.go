package subscription

import (
	"encoding/base64"
	"strings"
	"testing"
)

const testSSLink = "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@node1.example.com:8388#HK 01"

func TestDecodeSubscription_Base64AndPlain(t *testing.T) {
	plain := testSSLink + "\ntrojan://pw@node2.example.com:443#JP 01"

	encoded := base64.StdEncoding.EncodeToString([]byte(plain))
	if got := DecodeSubscription([]byte(encoded)); got != plain {
		t.Errorf("padded base64: got %q, want original content", got)
	}

	raw := base64.RawStdEncoding.EncodeToString([]byte(plain))
	if got := DecodeSubscription([]byte(raw)); got != plain {
		t.Errorf("raw base64: got %q, want original content", got)
	}

	// 明文多行原样返回
	if got := DecodeSubscription([]byte(plain)); got != plain {
		t.Errorf("plain content should pass through, got %q", got)
	}
}

func TestParseWithStats_Failures(t *testing.T) {
	content := strings.Join([]string{
		testSSLink,          // line 1: ok
		"",                  // line 2: empty,不计数
		"not-a-node",        // line 3: 失败
		"http://bad-prefix", // line 4: 失败
		"剩余流量 100GB",      // line 5: 无法解析(非协议前缀),失败
	}, "\n")

	res := ParseWithStats(content, "测试机场")
	if len(res.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(res.Nodes))
	}
	if res.ParseFailures != 3 {
		t.Errorf("ParseFailures = %d, want 3", res.ParseFailures)
	}
	if len(res.Failures) != 3 {
		t.Fatalf("Failures len = %d, want 3", len(res.Failures))
	}
	wantLines := []int{3, 4, 5}
	for i, f := range res.Failures {
		if f.Line != wantLines[i] {
			t.Errorf("Failures[%d].Line = %d, want %d", i, f.Line, wantLines[i])
		}
		if f.Reason == "" {
			t.Errorf("Failures[%d].Reason empty", i)
		}
	}
}

func TestParseWithStats_FailuresCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxLineFailures+50; i++ {
		b.WriteString("garbage-line\n")
	}
	res := ParseWithStats(b.String(), "测试机场")
	if res.ParseFailures != maxLineFailures+50 {
		t.Errorf("ParseFailures = %d, want %d (全量计数不截断)", res.ParseFailures, maxLineFailures+50)
	}
	if len(res.Failures) != maxLineFailures {
		t.Errorf("Failures len = %d, want capped %d", len(res.Failures), maxLineFailures)
	}
}

func TestDedupeByNodeKey_LastWins(t *testing.T) {
	n1 := &Node{Name: "A", Server: "node1.example.com", Port: 443, Password: "first"}
	n2 := &Node{Name: "B", Server: "node2.example.com", Port: 443, Password: "keep"}
	n3 := &Node{Name: "A-dup", Server: "node1.example.com", Port: 443, Password: "last"}

	out := DedupeByNodeKey([]*Node{n1, n2, n3})
	if len(out) != 2 {
		t.Fatalf("deduped = %d, want 2", len(out))
	}
	if out[0].Password != "last" {
		t.Errorf("out[0].Password = %q, want %q (后条覆盖前条)", out[0].Password, "last")
	}
	if out[1].Password != "keep" {
		t.Errorf("out[1].Password = %q, want %q", out[1].Password, "keep")
	}
	// 入参不被修改
	if n1.Password != "first" {
		t.Errorf("input mutated: n1.Password = %q", n1.Password)
	}
}

