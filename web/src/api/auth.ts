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
export interface LoginResponse {
  role?: string
  user?: LoginUser
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
