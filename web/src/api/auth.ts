// Login / captcha API wrapper (ticket 01 backend, ticket 04 frontend).
//
// Wire contract (internal/server/handlers_captcha.go + server.go handleLogin):
// - GET /api/captcha is UNAUTHENTICATED and returns {challenge_id, image_base64};
//   image_base64 is already a data URI, usable straight as an <img src>.
//   429 means the per-IP issue throttle tripped.
// - POST /api/login accepts optional captcha_id / captcha_answer. A 401 body is
//   JSON; captcha_required:true means every later login from this IP must carry
//   a captcha. Both "wrong password" and "wrong captcha" 401s may carry it.
import client from './client'

// CaptchaChallenge is one issued challenge: opaque id plus a ready-to-render
// data URI for the image.
export interface CaptchaChallenge {
  challenge_id: string
  image_base64: string
}

// LoginPayload is the body of POST /api/login. Captcha fields are omitted
// until the backend has told us they are required.
export interface LoginPayload {
  username: string
  password: string
  captcha_id?: string
  captcha_answer?: string
}

// LoginUser is the profile carried by a successful login response. role drives
// admin UI gating; must_change_password routes to the forced change screen and
// must_enroll_mfa (ticket 08) to the forced authenticator binding screen.
export interface LoginUser {
  role?: string
  must_change_password?: boolean
  must_enroll_mfa?: boolean
}

// LoginResponse tolerates both the nested user payload (current backend) and a
// flat role field (older shape), matching what the view already handled.
//
// The MFA handoff (ticket 06) rides the SAME 200 response: password accepted
// but the address is not trusted yet -> {ok:false, mfa_required:true,
// mfa_pending_token}. It is deliberately not an error status, so the view must
// branch on the body rather than on the catch block.
export interface LoginResponse {
  ok?: boolean
  role?: string
  user?: LoginUser
  mfa_required?: boolean
  mfa_pending_token?: string
}

// LoginMFAPayload is the body of POST /api/login/mfa. code carries either a
// 6-digit TOTP or a recovery code - one field, the backend tries TOTP first
// and recovery second. trust_ip requests the 30 day per-IP exemption.
export interface LoginMFAPayload {
  mfa_pending_token: string
  code: string
  trust_ip?: boolean
}

// issueCaptcha requests a fresh challenge. Only called after the backend has
// signalled captcha_required: the happy login path issues zero extra requests.
// skipErrorToast: the login view owns the wording (throttle vs. generic).
export function issueCaptcha(): Promise<CaptchaChallenge> {
  return client.get<unknown, CaptchaChallenge>('/captcha', { skipErrorToast: true })
}

// login submits credentials plus, when required, the captcha answer.
// skipAuthRedirect: a failed login must NOT bounce through the global 401
// redirect - that reload would wipe the captcha challenge we are about to
// render. skipErrorToast: the view maps 401/403 bodies onto local messages.
export function login(payload: LoginPayload): Promise<LoginResponse> {
  return client.post<unknown, LoginResponse>('/login', payload, {
    skipAuthRedirect: true,
    skipErrorToast: true
  })
}

// submitLoginMFA completes the second stage. Same two escapes as login():
// skipAuthRedirect because a wrong code answers 401 and a reload would throw
// away the pending token we are still allowed to retry with; skipErrorToast
// because every 401 here is deliberately indistinguishable on the wire (expired
// token, foreign IP, budget exhausted, wrong code) and the view owns the copy.
export function submitLoginMFA(payload: LoginMFAPayload): Promise<LoginResponse> {
  return client.post<unknown, LoginResponse>('/login/mfa', payload, {
    skipAuthRedirect: true,
    skipErrorToast: true
  })
}
