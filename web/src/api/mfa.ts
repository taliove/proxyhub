// MFA enrollment API wrapper (ticket 05 backend, ticket 08 frontend).
//
// Wire contract (internal/server/handlers_mfa.go handleMFAEnroll) - one
// endpoint, two stages, distinguished only by the presence of totp_code:
//
//   POST /api/me/mfa/enroll {}                -> {secret, otpauth_url, ...}
//   POST /api/me/mfa/enroll {totp_code:"..."} -> {ok, recovery_codes:[...]}
//
// Stage one stages a secret without enabling MFA, so it is safe to repeat
// (a page reload just re-provisions; the old staged secret goes inert).
// Stage two is the only moment the plaintext recovery codes exist: the server
// keeps SHA-256 digests only, so nothing can hand them out a second time.
//
// Both calls run with skipErrorToast: the enrollment view owns the wording
// (see views/mfa-enroll-utils.ts) because the backend errors are English
// sentinels like "invalid verification code".
import client from './client'

// MFAEnrollStart is the stage-one response. secret is the base32 seed for
// manual entry; otpauth_url is the otpauth://totp/... URI we render as a QR.
// The remaining fields are informational (the authenticator reads them from
// the URI) and stay optional so a leaner backend response still type-checks.
export interface MFAEnrollStart {
  secret: string
  otpauth_url: string
  digits?: number
  period?: number
  issuer?: string
  account_name?: string
}

// MFAEnrollConfirm is the stage-two response carrying the one-time recovery
// codes. Treat this payload as unrecoverable: it is never served again.
export interface MFAEnrollConfirm {
  ok?: boolean
  recovery_codes: string[]
}

// startMFAEnroll provisions and stages a fresh TOTP secret.
export function startMFAEnroll(): Promise<MFAEnrollStart> {
  return client.post<unknown, MFAEnrollStart>('/me/mfa/enroll', {}, { skipErrorToast: true })
}

// confirmMFAEnroll verifies the authenticator code against the staged secret
// and, on success, enables MFA and returns the recovery codes.
export function confirmMFAEnroll(totpCode: string): Promise<MFAEnrollConfirm> {
  return client.post<unknown, MFAEnrollConfirm>(
    '/me/mfa/enroll',
    { totp_code: totpCode },
    { skipErrorToast: true }
  )
}

// regenerateRecoveryCodes replaces the caller's whole recovery-code batch
// (POST /api/me/mfa/regenerate-recovery, handlers_mfa.go).
//
// code is a MANDATORY second-factor confirmation - either a current TOTP or an
// unused recovery code. The backend rejects an empty one with 400: without it a
// hijacked session could mint fresh long-lived credentials and silently
// invalidate the codes the real owner is holding.
//
// The previous batch is invalidated wholesale, and the returned plaintext is
// the only copy that will ever exist (the server keeps digests only).
// skipErrorToast: the caller localizes the sentinel English errors itself.
export function regenerateRecoveryCodes(code: string): Promise<MFAEnrollConfirm> {
  return client.post<unknown, MFAEnrollConfirm>(
    '/me/mfa/regenerate-recovery',
    { code },
    { skipErrorToast: true }
  )
}
