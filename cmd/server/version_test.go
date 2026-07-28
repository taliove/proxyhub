package main

// versionString 输出格式测试:buildTime 注入与否两种形态。
import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	oldVersion, oldBuildTime := version, buildTime
	t.Cleanup(func() { version, buildTime = oldVersion, oldBuildTime })

	version, buildTime = "1.2.3", "2026-07-28_00:00:00"
	got := versionString()
	if !strings.Contains(got, "1.2.3") || !strings.Contains(got, "2026-07-28_00:00:00") {
		t.Errorf("versionString() = %q, want both version and build time", got)
	}

	buildTime = ""
	got = versionString()
	if !strings.Contains(got, "1.2.3") {
		t.Errorf("versionString() = %q, want version", got)
	}
	if strings.Contains(got, "built") {
		t.Errorf("versionString() = %q, want no build-time suffix when unset", got)
	}
}
