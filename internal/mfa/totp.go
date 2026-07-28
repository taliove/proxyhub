// Package mfa implements the credential primitives behind multi-factor
// authentication: TOTP secret provisioning/verification and single-use
// recovery codes. It is deliberately storage-agnostic and knows nothing
// about HTTP or SQL, so both the server layer and the CLI can reuse it.
package mfa

import (
	"errors"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const (
	// Issuer is the label authenticator apps show for ProxyHub accounts.
	Issuer = "ProxyHub"
	// TOTPPeriod is the length of one TOTP time step.
	TOTPPeriod = 30 * time.Second
	// totpSkew tolerates one step on either side of the current window,
	// covering modest clock drift between server and phone.
	totpSkew = 1
	// totpSecretSize is the raw secret length in bytes (base32-encoded for
	// storage and provisioning), matching the RFC 4226 recommendation.
	totpSecretSize = 20
)

// ErrEmptyAccountName is returned when provisioning is asked for a secret
// without an account label to embed in the otpauth URL.
var ErrEmptyAccountName = errors.New("mfa: account name is required")

// TOTPKey is the result of provisioning a new authenticator secret. Secret
// is the base32 value to persist; OTPAuthURL is handed to the frontend,
// which renders the QR code itself (no QR dependency on the server).
type TOTPKey struct {
	Secret     string
	OTPAuthURL string
}

// GenerateTOTPSecret provisions a fresh TOTP secret for accountName. The
// secret is not active until the caller verifies a code against it and
// persists it (two-stage enrollment).
func GenerateTOTPSecret(accountName string) (TOTPKey, error) {
	accountName = strings.TrimSpace(accountName)
	if accountName == "" {
		return TOTPKey{}, ErrEmptyAccountName
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      Issuer,
		AccountName: accountName,
		Period:      uint(TOTPPeriod / time.Second),
		SecretSize:  totpSecretSize,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return TOTPKey{}, err
	}
	return TOTPKey{Secret: key.Secret(), OTPAuthURL: key.String()}, nil
}

// VerifyTOTP checks code against secret at the current time, tolerating one
// adjacent time window on either side.
func VerifyTOTP(secret, code string) bool {
	return VerifyTOTPAt(secret, code, time.Now())
}

// VerifyTOTPAt is VerifyTOTP with an explicit reference time (tests, and
// any caller that needs a fixed clock).
func VerifyTOTPAt(secret, code string, at time.Time) bool {
	secret = strings.TrimSpace(secret)
	code = normalizeTOTPCode(code)
	if secret == "" || code == "" {
		return false
	}

	ok, err := totp.ValidateCustom(code, secret, at.UTC(), totp.ValidateOpts{
		Period:    uint(TOTPPeriod / time.Second),
		Skew:      totpSkew,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return false
	}
	return ok
}

// normalizeTOTPCode strips the whitespace users paste along with a code
// (including the "123 456" grouping some apps display).
func normalizeTOTPCode(code string) string {
	var b strings.Builder
	b.Grow(len(code))
	for _, r := range code {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
