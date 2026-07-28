package mfa

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var recoveryCodeShape = regexp.MustCompile(`^[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{4}-[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{4}-[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{4}$`)

func TestGenerateRecoveryCodesShape(t *testing.T) {
	plain, stored, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(plain) != RecoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(plain), RecoveryCodeCount)
	}

	seen := make(map[string]bool, len(plain))
	for _, code := range plain {
		if !recoveryCodeShape.MatchString(code) {
			t.Fatalf("code %q does not match XXXX-XXXX-XXXX with the safe charset", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code generated: %q", code)
		}
		seen[code] = true
	}

	var hashes []string
	if err := json.Unmarshal([]byte(stored), &hashes); err != nil {
		t.Fatalf("stored form is not a JSON array: %v (%q)", err, stored)
	}
	if len(hashes) != RecoveryCodeCount {
		t.Fatalf("stored %d hashes, want %d", len(hashes), RecoveryCodeCount)
	}
	for i, code := range plain {
		sum := sha256.Sum256([]byte(code))
		want := hex.EncodeToString(sum[:])
		if hashes[i] != want {
			t.Fatalf("hash[%d] = %q, want sha256(%q) = %q", i, hashes[i], code, want)
		}
		if strings.Contains(stored, code) {
			t.Fatalf("stored form leaks plaintext code %q", code)
		}
	}
}

func TestGenerateRecoveryCodesAreRandomAcrossCalls(t *testing.T) {
	first, _, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	second, _, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	overlap := 0
	for _, a := range first {
		for _, b := range second {
			if a == b {
				overlap++
			}
		}
	}
	if overlap > 0 {
		t.Fatalf("%d codes repeated between two generations", overlap)
	}
}

func TestVerifyRecoveryCodeConsumesOnlyMatchingCode(t *testing.T) {
	plain, stored, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}

	remaining, ok, err := VerifyRecoveryCode(stored, plain[3])
	if err != nil {
		t.Fatalf("VerifyRecoveryCode: %v", err)
	}
	if !ok {
		t.Fatal("valid code rejected")
	}

	count, err := RemainingRecoveryCodes(remaining)
	if err != nil {
		t.Fatalf("RemainingRecoveryCodes: %v", err)
	}
	if count != RecoveryCodeCount-1 {
		t.Fatalf("remaining = %d, want %d", count, RecoveryCodeCount-1)
	}

	// The consumed code must not work again.
	if _, ok, err := VerifyRecoveryCode(remaining, plain[3]); err != nil || ok {
		t.Fatalf("consumed code accepted again (ok=%v err=%v)", ok, err)
	}

	// Every other code still works against the reduced set.
	for i, code := range plain {
		if i == 3 {
			continue
		}
		if _, ok, err := VerifyRecoveryCode(remaining, code); err != nil || !ok {
			t.Fatalf("untouched code %q rejected (ok=%v err=%v)", code, ok, err)
		}
	}
}

func TestVerifyRecoveryCodeExhaustion(t *testing.T) {
	plain, stored, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	for _, code := range plain {
		next, ok, err := VerifyRecoveryCode(stored, code)
		if err != nil || !ok {
			t.Fatalf("code %q rejected (ok=%v err=%v)", code, ok, err)
		}
		stored = next
	}
	count, err := RemainingRecoveryCodes(stored)
	if err != nil {
		t.Fatalf("RemainingRecoveryCodes: %v", err)
	}
	if count != 0 {
		t.Fatalf("remaining = %d, want 0", count)
	}
}

func TestVerifyRecoveryCodeNormalizesInput(t *testing.T) {
	plain, stored, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	messy := " " + strings.ToLower(strings.ReplaceAll(plain[0], "-", "")) + "\n"
	if _, ok, err := VerifyRecoveryCode(stored, messy); err != nil || !ok {
		t.Fatalf("normalized input %q rejected (ok=%v err=%v)", messy, ok, err)
	}
}

func TestVerifyRecoveryCodeRejectsUnknownAndEmpty(t *testing.T) {
	_, stored, err := GenerateRecoveryCodes()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	for _, input := range []string{"", "   ", "AAAA-AAAA-AAAA", "not-a-code"} {
		remaining, ok, err := VerifyRecoveryCode(stored, input)
		if err != nil {
			t.Fatalf("VerifyRecoveryCode(%q) error: %v", input, err)
		}
		if ok {
			t.Fatalf("input %q accepted", input)
		}
		if remaining != stored {
			t.Fatalf("failed verification mutated the stored set")
		}
	}
}

func TestVerifyRecoveryCodeEmptyStore(t *testing.T) {
	for _, stored := range []string{"", "[]", "null"} {
		remaining, ok, err := VerifyRecoveryCode(stored, "AAAA-BBBB-CCCC")
		if err != nil {
			t.Fatalf("VerifyRecoveryCode(stored=%q) error: %v", stored, err)
		}
		if ok {
			t.Fatalf("empty store accepted a code (stored=%q)", stored)
		}
		if remaining != stored {
			t.Fatalf("empty store mutated: %q -> %q", stored, remaining)
		}
	}
}

func TestRecoveryCodeStoreMalformed(t *testing.T) {
	if _, _, err := VerifyRecoveryCode("{not json", "AAAA-BBBB-CCCC"); err == nil {
		t.Fatal("expected error for malformed stored hash set")
	}
	if _, err := RemainingRecoveryCodes("{not json"); err == nil {
		t.Fatal("expected error for malformed stored hash set")
	}
	if n, err := RemainingRecoveryCodes(""); err != nil || n != 0 {
		t.Fatalf("RemainingRecoveryCodes(\"\") = (%d, %v), want (0, nil)", n, err)
	}
}
