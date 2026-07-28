package mfa

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateTOTPSecret(t *testing.T) {
	got, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if got.Secret == "" {
		t.Fatal("secret is empty")
	}
	if len(got.Secret) < 16 {
		t.Fatalf("secret too short: %q", got.Secret)
	}
	u, err := url.Parse(got.OTPAuthURL)
	if err != nil {
		t.Fatalf("parse otpauth url %q: %v", got.OTPAuthURL, err)
	}
	if u.Scheme != "otpauth" || u.Host != "totp" {
		t.Fatalf("unexpected otpauth prefix: %q", got.OTPAuthURL)
	}
	if want := "/" + Issuer + ":alice"; u.Path != want {
		t.Fatalf("otpauth path = %q, want %q", u.Path, want)
	}
	q := u.Query()
	if q.Get("issuer") != Issuer {
		t.Fatalf("issuer = %q, want %q", q.Get("issuer"), Issuer)
	}
	if q.Get("secret") != got.Secret {
		t.Fatalf("url secret = %q, want %q", q.Get("secret"), got.Secret)
	}
}

func TestGenerateTOTPSecretRejectsEmptyAccount(t *testing.T) {
	if _, err := GenerateTOTPSecret(""); err == nil {
		t.Fatal("expected error for empty account name")
	}
}

func TestGenerateTOTPSecretIsRandom(t *testing.T) {
	a, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	b, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if a.Secret == b.Secret {
		t.Fatal("two generated secrets are identical")
	}
}

func TestVerifyTOTPAcceptsAdjacentWindows(t *testing.T) {
	key, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	now := time.Now()

	for _, offset := range []time.Duration{-TOTPPeriod, 0, TOTPPeriod} {
		code, err := totp.GenerateCode(key.Secret, now.Add(offset))
		if err != nil {
			t.Fatalf("GenerateCode(offset=%v): %v", offset, err)
		}
		if !VerifyTOTPAt(key.Secret, code, now) {
			t.Fatalf("code for offset %v rejected, want accepted", offset)
		}
	}
}

func TestVerifyTOTPRejectsDistantWindows(t *testing.T) {
	key, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	now := time.Now()

	for _, offset := range []time.Duration{-3 * TOTPPeriod, 2 * TOTPPeriod, 10 * time.Minute} {
		code, err := totp.GenerateCode(key.Secret, now.Add(offset))
		if err != nil {
			t.Fatalf("GenerateCode(offset=%v): %v", offset, err)
		}
		if VerifyTOTPAt(key.Secret, code, now) {
			t.Fatalf("code for offset %v accepted, want rejected", offset)
		}
	}
}

func TestVerifyTOTPRejectsGarbage(t *testing.T) {
	key, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	cases := []string{"", "000000", "abcdef", "12345", "1234567"}
	for _, code := range cases {
		if VerifyTOTP(key.Secret, code) {
			t.Fatalf("garbage code %q accepted", code)
		}
	}
	if VerifyTOTP("", "123456") {
		t.Fatal("empty secret accepted a code")
	}
}

func TestVerifyTOTPTolerantOfUserFormatting(t *testing.T) {
	key, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	now := time.Now()
	code, err := totp.GenerateCode(key.Secret, now)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	spaced := " " + code[:3] + " " + code[3:] + "\n"
	if !VerifyTOTPAt(key.Secret, spaced, now) {
		t.Fatalf("code with surrounding whitespace rejected: %q", spaced)
	}
}

func TestVerifyTOTPCurrentTimeWrapper(t *testing.T) {
	key, err := GenerateTOTPSecret("alice")
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	code, err := totp.GenerateCode(key.Secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if !VerifyTOTP(key.Secret, code) {
		t.Fatal("current window code rejected by VerifyTOTP")
	}
	if strings.TrimSpace(code) == "" {
		t.Fatal("generated code is blank")
	}
}
