package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestGetSettingForUser_FallbackChain 租户级设置回退链:
// 用户覆盖优先;删除覆盖回退全局默认;两者皆无返回 ErrNotFound(调用方给内置默认)。
func TestGetSettingForUser_FallbackChain(t *testing.T) {
	st, err := OpenForTesting(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	const key = "filter_keywords"
	// 全局默认
	if err := st.SetSetting(key, "global-bad"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	// 无覆盖:回退全局
	if v, err := st.GetSettingForUser(2, key); err != nil || v != "global-bad" {
		t.Fatalf("fallback = %q, %v, want global-bad", v, err)
	}
	// 用户覆盖:优先
	if err := st.SetUserSetting(2, key, "user-bad"); err != nil {
		t.Fatalf("set user: %v", err)
	}
	if v, err := st.GetSettingForUser(2, key); err != nil || v != "user-bad" {
		t.Fatalf("override = %q, %v, want user-bad", v, err)
	}
	// 另一用户不受覆盖影响
	if v, err := st.GetSettingForUser(3, key); err != nil || v != "global-bad" {
		t.Fatalf("other user = %q, %v, want global-bad", v, err)
	}
	// 重置(删覆盖):回到跟随全局
	if err := st.DeleteUserSetting(2, key); err != nil {
		t.Fatalf("delete override: %v", err)
	}
	if v, err := st.GetSettingForUser(2, key); err != nil || v != "global-bad" {
		t.Fatalf("after reset = %q, %v, want global-bad", v, err)
	}
	// 两者皆无:ErrNotFound
	if _, err := st.GetSettingForUser(2, "nonexistent_key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key err = %v, want ErrNotFound", err)
	}
	// userID=0 只读全局(不读用户覆盖)
	if v, err := st.GetSettingForUser(0, key); err != nil || v != "global-bad" {
		t.Fatalf("uid0 = %q, %v, want global-bad", v, err)
	}
}

// TestClashTemplateForUser_FallbackChain 模板回退链:
// 用户覆盖优先;删除覆盖回退全局默认;全局也无回退内嵌默认。
func TestClashTemplateForUser_FallbackChain(t *testing.T) {
	st, err := OpenForTesting(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.SetClashTemplate("global-template"); err != nil {
		t.Fatalf("set global: %v", err)
	}
	// 无覆盖:全局
	if v, err := st.GetClashTemplateForUser(2); err != nil || v != "global-template" {
		t.Fatalf("fallback = %q, %v", v, err)
	}
	// 覆盖优先 + upsert 幂等(重复写只剩一行生效)
	if err := st.SetClashTemplateForUser(2, "user-template"); err != nil {
		t.Fatalf("set user: %v", err)
	}
	if err := st.SetClashTemplateForUser(2, "user-template-v2"); err != nil {
		t.Fatalf("set user v2: %v", err)
	}
	if v, err := st.GetClashTemplateForUser(2); err != nil || v != "user-template-v2" {
		t.Fatalf("override = %q, %v", v, err)
	}
	// 删除覆盖:回退全局
	if err := st.DeleteClashTemplateForUser(2); err != nil {
		t.Fatalf("delete override: %v", err)
	}
	if v, err := st.GetClashTemplateForUser(2); err != nil || v != "global-template" {
		t.Fatalf("after reset = %q, %v", v, err)
	}
}

// TestDetectionTargetsForUser_FallbackChain 检测目标回退链:覆盖优先,无覆盖回退全局。
func TestDetectionTargetsForUser_FallbackChain(t *testing.T) {
	st, err := OpenForTesting(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	if err := st.SeedDetectionTargets(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	global, err := st.GetDetectionTargetsForUser(2)
	if err != nil || len(global) == 0 {
		t.Fatalf("fallback targets = %+v, %v, want seeded defaults", global, err)
	}
	custom := global[:1]
	if err := st.SetDetectionTargetsForUser(2, custom); err != nil {
		t.Fatalf("set user: %v", err)
	}
	got, err := st.GetDetectionTargetsForUser(2)
	if err != nil || len(got) != 1 {
		t.Fatalf("override targets = %+v, %v, want 1 custom", got, err)
	}
	// 全局视角不受用户覆盖影响
	all, err := st.GetDetectionTargets()
	if err != nil || len(all) != len(global) {
		t.Fatalf("global targets changed by user override: %+v", all)
	}
}
