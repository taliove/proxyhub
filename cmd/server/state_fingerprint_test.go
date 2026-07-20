package main

import (
	"strings"
	"testing"
)

func TestReadAuthenticationKey(t *testing.T) {
	key, err := readAuthenticationKey(strings.NewReader("0123456789abcdef0123456789abcdef\n"))
	if err != nil {
		t.Fatalf("readAuthenticationKey() error = %v", err)
	}
	if string(key) != "0123456789abcdef0123456789abcdef" {
		t.Errorf("key = %q, want 原始 32 字节密钥", key)
	}
}

func TestReadAuthenticationKey_NoTrailingNewline(t *testing.T) {
	// EOF 前无换行也应可读(例如 printf 不带 \n)
	key, err := readAuthenticationKey(strings.NewReader("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("readAuthenticationKey() error = %v", err)
	}
	if len(key) != 32 {
		t.Errorf("len(key) = %d, want 32", len(key))
	}
}

func TestReadAuthenticationKey_KeepsInnerSpaces(t *testing.T) {
	// 密钥内部的空格是密钥材料,只允许剥离行尾换行
	raw := "0123456789abcdef 0123456789abcde\n" // 含 1 空格共 32 字节
	key, err := readAuthenticationKey(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("readAuthenticationKey() error = %v", err)
	}
	if string(key) != strings.TrimRight(raw, "\n") {
		t.Errorf("key = %q, want %q", key, strings.TrimRight(raw, "\n"))
	}
}

func TestReadAuthenticationKey_RejectsShortKey(t *testing.T) {
	if _, err := readAuthenticationKey(strings.NewReader("too-short\n")); err == nil {
		t.Error("短密钥 expected error, got nil")
	}
}

func TestReadAuthenticationKey_RejectsEmpty(t *testing.T) {
	if _, err := readAuthenticationKey(strings.NewReader("")); err == nil {
		t.Error("空输入 expected error, got nil")
	}
	if _, err := readAuthenticationKey(strings.NewReader("\n")); err == nil {
		t.Error("空行 expected error, got nil")
	}
}

func TestReadAuthenticationKey_StripsCRLF(t *testing.T) {
	key, err := readAuthenticationKey(strings.NewReader("0123456789abcdef0123456789abcdef\r\n"))
	if err != nil {
		t.Fatalf("readAuthenticationKey() error = %v", err)
	}
	if len(key) != 32 {
		t.Errorf("len(key) = %d, want 32 (CRLF 应被剥离)", len(key))
	}
}
