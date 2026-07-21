package subscription

import (
	"testing"
)

func TestParseWithStats_Success(t *testing.T) {
	content := `vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJhaWQiOjAsIm5ldCI6InRjcCIsInRscyI6InRscyIsInBzIjoiVGVzdCBOb2RlIn0=
vless://00000000-0000-0000-0000-000000000000@example.com:443?security=tls#Test%20VLess
trojan://password@example.com:443#Test%20Trojan
invalid-line-no-protocol

ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@example.com:8080#Test%20SS`

	result := ParseWithStats(content, "test-source")

	if result.TotalLines != 5 {
		t.Errorf("TotalLines = %d, want 5", result.TotalLines)
	}
	if result.ParseFailures != 1 {
		t.Errorf("ParseFailures = %d, want 1 (invalid-line-no-protocol)", result.ParseFailures)
	}
	if len(result.Nodes) != 4 {
		t.Errorf("Nodes count = %d, want 4", len(result.Nodes))
	}

	// Verify node types
	expectedTypes := []string{"vmess", "vless", "trojan", "ss"}
	for i, node := range result.Nodes {
		if node.Type != expectedTypes[i] {
			t.Errorf("Node[%d].Type = %s, want %s", i, node.Type, expectedTypes[i])
		}
		if node.Source != "test-source" {
			t.Errorf("Node[%d].Source = %s, want test-source", i, node.Source)
		}
	}
}

func TestParseWithStats_MetadataFiltering(t *testing.T) {
	content := `vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJhaWQiOjAsIm5ldCI6InRjcCIsInRscyI6InRscyIsInBzIjoi5Ymp5L2Z5rWB6YePIn0=
vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOjQ0MywiaWQiOiIwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAiLCJhaWQiOjAsIm5ldCI6InRjcCIsInRscyI6InRscyIsInBzIjoiVGVzdCBOb2RlIn0=`

	result := ParseWithStats(content, "test")

	// First node name is "剩余流量" (metadata), should be filtered
	if result.TotalLines != 2 {
		t.Errorf("TotalLines = %d, want 2", result.TotalLines)
	}
	if result.ParseFailures != 0 {
		t.Errorf("ParseFailures = %d, want 0", result.ParseFailures)
	}
	if len(result.Nodes) != 1 {
		t.Errorf("Nodes count = %d, want 1 (metadata filtered)", len(result.Nodes))
	}
}

func TestParseWithStats_EmptyContent(t *testing.T) {
	result := ParseWithStats("", "test")

	if result.TotalLines != 0 {
		t.Errorf("TotalLines = %d, want 0", result.TotalLines)
	}
	if result.ParseFailures != 0 {
		t.Errorf("ParseFailures = %d, want 0", result.ParseFailures)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("Nodes count = %d, want 0", len(result.Nodes))
	}
}

func TestParseWithStats_AllInvalid(t *testing.T) {
	content := `invalid-line-1
invalid-line-2
not-a-protocol://invalid`

	result := ParseWithStats(content, "test")

	if result.TotalLines != 3 {
		t.Errorf("TotalLines = %d, want 3", result.TotalLines)
	}
	if result.ParseFailures != 3 {
		t.Errorf("ParseFailures = %d, want 3", result.ParseFailures)
	}
	if len(result.Nodes) != 0 {
		t.Errorf("Nodes count = %d, want 0", len(result.Nodes))
	}
}
