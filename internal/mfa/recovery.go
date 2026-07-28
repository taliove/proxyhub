package mfa

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// RecoveryCodeCount is how many recovery codes one generation yields.
	RecoveryCodeCount = 10
	// recoveryGroupSize / recoveryGroups shape a code as XXXX-XXXX-XXXX.
	recoveryGroupSize = 4
	recoveryGroups    = 3
	// recoveryCharset drops the visually ambiguous glyphs (0/O, 1/I/L) so a
	// code read off paper cannot be mistyped into a different valid code.
	// 31 symbols over 12 positions is roughly 59 bits of entropy.
	recoveryCharset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
)

// GenerateRecoveryCodes produces a fresh batch of single-use recovery codes.
// The plaintext slice is the only time the codes exist in readable form -
// callers must show it to the user once and never persist it. The stored
// value is a JSON array of SHA-256 hex digests, ready to write to
// users.recovery_codes_hash.
func GenerateRecoveryCodes() (plaintext []string, stored string, err error) {
	codes := make([]string, 0, RecoveryCodeCount)
	hashes := make([]string, 0, RecoveryCodeCount)
	seen := make(map[string]bool, RecoveryCodeCount)

	for len(codes) < RecoveryCodeCount {
		code, err := randomRecoveryCode()
		if err != nil {
			return nil, "", err
		}
		if seen[code] {
			continue // vanishingly rare, but never hand out a duplicate
		}
		seen[code] = true
		codes = append(codes, code)
		hashes = append(hashes, hashRecoveryCode(code))
	}

	encoded, err := encodeRecoveryHashes(hashes)
	if err != nil {
		return nil, "", err
	}
	return codes, encoded, nil
}

// VerifyRecoveryCode checks input against the stored hash set. On success it
// returns the stored set with that code removed (single use); on failure the
// stored set is returned unchanged. A malformed stored value is an error so
// callers never silently treat corruption as "no codes left".
func VerifyRecoveryCode(stored, input string) (remaining string, ok bool, err error) {
	hashes, err := decodeRecoveryHashes(stored)
	if err != nil {
		return stored, false, err
	}

	normalized := normalizeRecoveryCode(input)
	if normalized == "" || len(hashes) == 0 {
		return stored, false, nil
	}
	target := hashRecoveryCode(normalized)

	matched := -1
	for i, h := range hashes {
		if subtle.ConstantTimeCompare([]byte(h), []byte(target)) == 1 {
			matched = i
			break
		}
	}
	if matched < 0 {
		return stored, false, nil
	}

	// Build a new slice instead of mutating the decoded one in place.
	kept := make([]string, 0, len(hashes)-1)
	kept = append(kept, hashes[:matched]...)
	kept = append(kept, hashes[matched+1:]...)

	encoded, err := encodeRecoveryHashes(kept)
	if err != nil {
		return stored, false, err
	}
	return encoded, true, nil
}

// RemainingRecoveryCodes reports how many unused codes the stored set holds.
func RemainingRecoveryCodes(stored string) (int, error) {
	hashes, err := decodeRecoveryHashes(stored)
	if err != nil {
		return 0, err
	}
	return len(hashes), nil
}

// randomRecoveryCode draws one XXXX-XXXX-XXXX code from crypto/rand.
func randomRecoveryCode() (string, error) {
	total := recoveryGroupSize * recoveryGroups
	picked := make([]byte, 0, total)
	for len(picked) < total {
		buf := make([]byte, total)
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("mfa: read random bytes: %w", err)
		}
		// Rejection sampling keeps the charset uniform: 248 is the largest
		// multiple of 31 below 256, so bytes above it are discarded.
		const limit = 248
		for _, b := range buf {
			if b >= limit {
				continue
			}
			picked = append(picked, recoveryCharset[int(b)%len(recoveryCharset)])
			if len(picked) == total {
				break
			}
		}
	}

	groups := make([]string, recoveryGroups)
	for i := range groups {
		groups[i] = string(picked[i*recoveryGroupSize : (i+1)*recoveryGroupSize])
	}
	return strings.Join(groups, "-"), nil
}

// normalizeRecoveryCode accepts whatever the user typed (lowercase, missing
// or extra dashes, stray whitespace) and rebuilds the canonical form. It
// returns "" when the input cannot be a recovery code.
func normalizeRecoveryCode(input string) string {
	var b strings.Builder
	b.Grow(recoveryGroupSize * recoveryGroups)
	for _, r := range strings.ToUpper(input) {
		if strings.ContainsRune(recoveryCharset, r) {
			b.WriteRune(r)
		}
	}
	raw := b.String()
	if len(raw) != recoveryGroupSize*recoveryGroups {
		return ""
	}

	groups := make([]string, recoveryGroups)
	for i := range groups {
		groups[i] = raw[i*recoveryGroupSize : (i+1)*recoveryGroupSize]
	}
	return strings.Join(groups, "-")
}

// hashRecoveryCode returns the hex SHA-256 digest stored for a code.
func hashRecoveryCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func encodeRecoveryHashes(hashes []string) (string, error) {
	if hashes == nil {
		hashes = []string{}
	}
	b, err := json.Marshal(hashes)
	if err != nil {
		return "", fmt.Errorf("mfa: encode recovery hashes: %w", err)
	}
	return string(b), nil
}

// decodeRecoveryHashes parses the stored JSON array. An empty string and a
// JSON null both mean "no codes" (a user who never enrolled).
func decodeRecoveryHashes(stored string) ([]string, error) {
	trimmed := strings.TrimSpace(stored)
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var hashes []string
	if err := json.Unmarshal([]byte(trimmed), &hashes); err != nil {
		return nil, fmt.Errorf("mfa: decode recovery hashes: %w", err)
	}
	return hashes, nil
}
