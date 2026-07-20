package store

import (
	"strings"
	"testing"
)

func TestClashTemplate_DefaultFallback(t *testing.T) {
	st := newTestStore(t)

	// 从未保存过：应回退到默认模板（非空）
	tmpl, err := st.GetClashTemplate()
	if err != nil {
		t.Fatalf("GetClashTemplate() error = %v", err)
	}
	if tmpl == "" {
		t.Fatal("GetClashTemplate() returned empty, want default template")
	}
	if !strings.Contains(tmpl, "{{nodes}}") {
		t.Error("default template should contain {{nodes}} placeholder")
	}
}

func TestClashTemplate_SaveAndGet(t *testing.T) {
	st := newTestStore(t)

	custom := "mode: rule\nproxy-groups: []\nrules: [MATCH,DIRECT]\n"
	if err := st.SetClashTemplate(custom); err != nil {
		t.Fatalf("SetClashTemplate() error = %v", err)
	}

	got, err := st.GetClashTemplate()
	if err != nil {
		t.Fatalf("GetClashTemplate() error = %v", err)
	}
	if got != custom {
		t.Errorf("GetClashTemplate() = %q, want %q", got, custom)
	}
}

func TestClashTemplate_Reset(t *testing.T) {
	st := newTestStore(t)

	if err := st.SetClashTemplate("mode: rule\n"); err != nil {
		t.Fatalf("SetClashTemplate() error = %v", err)
	}
	if err := st.ResetClashTemplate(); err != nil {
		t.Fatalf("ResetClashTemplate() error = %v", err)
	}

	got, err := st.GetClashTemplate()
	if err != nil {
		t.Fatalf("GetClashTemplate() error = %v", err)
	}
	if !strings.Contains(got, "{{nodes}}") {
		t.Error("after reset, template should be the default (containing {{nodes}})")
	}
}

func TestClashTemplate_EmptyRejected(t *testing.T) {
	st := newTestStore(t)
	if err := st.SetClashTemplate(""); err == nil {
		t.Fatal("SetClashTemplate(\"\") expected error, got nil")
	}
}
