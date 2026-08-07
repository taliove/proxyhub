package store

import "testing"

// issue #83:node_overrides 覆盖层新增 favorite 列(节点收藏)的读写 roundtrip。
// fixture 全合成:example.com + 全零 UUID 语义,不触网。

func TestNodeFavorite_SetAndListRoundtrip(t *testing.T) {
	st := newTestStore(t)

	// 初始无收藏
	favs, err := st.ListFavoriteNodeKeysForUser(1)
	if err != nil {
		t.Fatalf("ListFavoriteNodeKeysForUser() error = %v", err)
	}
	if len(favs) != 0 {
		t.Fatalf("len(favs) = %d, want 0 (fresh store)", len(favs))
	}

	// 收藏 -> 列表命中
	if err := st.SetNodeFavoriteForUser(1, "example.com:443", true); err != nil {
		t.Fatalf("SetNodeFavoriteForUser(true) error = %v", err)
	}
	favs, err = st.ListFavoriteNodeKeysForUser(1)
	if err != nil {
		t.Fatalf("ListFavoriteNodeKeysForUser() error = %v", err)
	}
	if !favs["example.com:443"] {
		t.Errorf("favs missing key after favorite=true: %v", favs)
	}

	// 覆盖层读取同样带 favorite 标记
	ovs, err := st.ListNodeOverridesForUser(1)
	if err != nil {
		t.Fatalf("ListNodeOverridesForUser() error = %v", err)
	}
	if !ovs["example.com:443"].Favorite {
		t.Errorf("override Favorite = false, want true")
	}

	// 取消收藏 -> 列表不再命中(覆盖行保留,favorite=false)
	if err := st.SetNodeFavoriteForUser(1, "example.com:443", false); err != nil {
		t.Fatalf("SetNodeFavoriteForUser(false) error = %v", err)
	}
	favs, _ = st.ListFavoriteNodeKeysForUser(1)
	if favs["example.com:443"] {
		t.Errorf("favs still contains key after favorite=false: %v", favs)
	}
}

func TestNodeFavorite_PreservesOverrideFields(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:8388"

	// 先写名称/地区覆盖,再收藏:收藏不得清掉 display_name/region。
	if err := st.SetNodeOverrideForUser(1, key, "别名A", "HK"); err != nil {
		t.Fatalf("SetNodeOverrideForUser() error = %v", err)
	}
	if err := st.SetNodeFavoriteForUser(1, key, true); err != nil {
		t.Fatalf("SetNodeFavoriteForUser() error = %v", err)
	}
	ovs, _ := st.ListNodeOverridesForUser(1)
	ov := ovs[key]
	if ov.DisplayName != "别名A" || ov.Region != "HK" || !ov.Favorite {
		t.Errorf("override after favorite = %+v, want 别名A/HK/favorite", ov)
	}

	// 反向:再改名称覆盖,不得清掉 favorite。
	if err := st.SetNodeOverrideForUser(1, key, "别名B", "JP"); err != nil {
		t.Fatalf("SetNodeOverrideForUser() error = %v", err)
	}
	ovs, _ = st.ListNodeOverridesForUser(1)
	ov = ovs[key]
	if ov.DisplayName != "别名B" || ov.Region != "JP" || !ov.Favorite {
		t.Errorf("override after rename = %+v, want 别名B/JP/favorite", ov)
	}
}

func TestNodeFavorite_PerUserIsolation(t *testing.T) {
	st := newTestStore(t)
	key := "example.com:443"

	if err := st.SetNodeFavoriteForUser(1, key, true); err != nil {
		t.Fatalf("SetNodeFavoriteForUser() error = %v", err)
	}

	// 用户 2 视角不可见用户 1 的收藏
	favs2, err := st.ListFavoriteNodeKeysForUser(2)
	if err != nil {
		t.Fatalf("ListFavoriteNodeKeysForUser() error = %v", err)
	}
	if favs2[key] {
		t.Errorf("user 2 sees user 1 favorite: %v", favs2)
	}

	// 用户 2 独立收藏同一节点,互不串扰
	if err := st.SetNodeFavoriteForUser(2, key, true); err != nil {
		t.Fatalf("SetNodeFavoriteForUser() error = %v", err)
	}
	if err := st.SetNodeFavoriteForUser(1, key, false); err != nil {
		t.Fatalf("SetNodeFavoriteForUser() error = %v", err)
	}
	favs2, _ = st.ListFavoriteNodeKeysForUser(2)
	if !favs2[key] {
		t.Errorf("user 2 favorite lost after user 1 unfavorite: %v", favs2)
	}
}

// Clear 语义分叉(check 评审 MEDIUM-2):已收藏行清名称覆盖时保留收藏(只清字段),
// 未收藏行维持整行删除的旧语义。
func TestNodeFavorite_ClearPreservesFavorite(t *testing.T) {
	st := newTestStore(t)
	favKey := "fav.example.com:8388"
	plainKey := "plain.example.com:8388"

	// 已收藏行:清名称覆盖 → 行保留,名称/地区清空,收藏仍在
	if err := st.SetNodeOverrideForUser(1, favKey, "别名", "HK"); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if err := st.SetNodeFavoriteForUser(1, favKey, true); err != nil {
		t.Fatalf("set favorite: %v", err)
	}
	if err := st.ClearNodeOverrideForUser(1, favKey); err != nil {
		t.Fatalf("clear: %v", err)
	}
	ovs, _ := st.ListNodeOverridesForUser(1)
	ov, ok := ovs[favKey]
	if !ok {
		t.Fatal("已收藏行被整行删除,want 保留")
	}
	if ov.DisplayName != "" || ov.Region != "" {
		t.Errorf("DisplayName/Region = %q/%q, want 均清空", ov.DisplayName, ov.Region)
	}
	if !ov.Favorite {
		t.Error("Favorite 被清掉,want 保留")
	}

	// 未收藏行:清名称覆盖 → 整行删除(旧语义回归)
	if err := st.SetNodeOverrideForUser(1, plainKey, "别名", "HK"); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if err := st.ClearNodeOverrideForUser(1, plainKey); err != nil {
		t.Fatalf("clear: %v", err)
	}
	ovs, _ = st.ListNodeOverridesForUser(1)
	if _, ok := ovs[plainKey]; ok {
		t.Error("未收藏行仍在,want 整行删除")
	}
}
