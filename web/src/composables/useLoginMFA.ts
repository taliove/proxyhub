// Second login stage state machine (ticket 09).
//
// Lifecycle, driven entirely by the backend:
//   1. page load: dormant. The login page renders only the password form.
//   2. POST /api/login answers 200 {ok:false, mfa_required:true,
//      mfa_pending_token} -> start(token): the page swaps to the code form.
//   3. submit() posts {mfa_pending_token, code, trust_ip} and hands the login
//      response back to the view, which owns the routing (change-password /
//      enroll / home) so both stages share exactly one branch.
//   4. any failure clears the code and keeps the handoff retryable, except the
//      statuses that provably killed it server-side (see isLoginMFASessionLost),
//      which reset() us back to the password form.
//
// The pending token lives only here: never in localStorage, never in the URL.
// It is a 5 minute, IP-bound, 5-attempt bearer credential - a reload is
// supposed to lose it and send the user back to the password form.
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { submitLoginMFA, type LoginResponse } from '@/api/auth'
import {
  TOTP_CODE_LENGTH,
  isCompleteLoginMFACode,
  isLoginMFASessionLost,
  loginMFAErrorMessage,
  normalizeLoginMFACode,
  type LoginMFAMode
} from '@/views/login-mfa-utils'

export function useLoginMFA() {
  // pendingToken empty means dormant: there is no open handoff.
  const pendingToken = ref('')
  const code = ref('')
  const mode = ref<LoginMFAMode>('totp')
  const trustIP = ref(false)
  const submitting = ref(false)

  const active = computed(() => pendingToken.value !== '')
  const codeComplete = computed(() => isCompleteLoginMFACode(code.value, mode.value))

  // setCode is the single write path for the input. The views bind
  // :model-value + @update:model-value through it instead of v-model so the
  // normalized value is what is both displayed and submitted.
  function setCode(raw: string): void {
    code.value = normalizeLoginMFACode(raw, mode.value)
  }

  // start opens the handoff handed out by the password stage.
  function start(token: string): void {
    pendingToken.value = token
    code.value = ''
    mode.value = 'totp'
    trustIP.value = false
  }

  // reset drops the handoff and returns the page to the password form. Called
  // by the "back to sign in" affordance and whenever the handoff is provably
  // dead. The trust_ip preference is intentionally dropped with it: it was a
  // choice about this one verification, not a sticky setting.
  function reset(): void {
    pendingToken.value = ''
    code.value = ''
    mode.value = 'totp'
    trustIP.value = false
  }

  // switchMode flips between authenticator code and recovery code, clearing
  // the box: the two formats share no prefix, so keeping half a TOTP around
  // would only produce an invalid recovery code.
  function switchMode(next: LoginMFAMode): void {
    if (mode.value === next) return
    mode.value = next
    code.value = ''
  }

  function toggleMode(): void {
    switchMode(mode.value === 'totp' ? 'recovery' : 'totp')
  }

  // submit sends the code. Returns the login response on success and null on
  // any failure (already reported to the user). Never throws: the caller is a
  // form handler, and every backend failure here is expected traffic.
  async function submit(): Promise<LoginResponse | null> {
    if (!active.value) return null
    if (!codeComplete.value) {
      ElMessage.warning(
        mode.value === 'totp'
          ? `请输入认证器上的 ${TOTP_CODE_LENGTH} 位验证码`
          : '请输入完整的恢复码(XXXX-XXXX-XXXX)'
      )
      return null
    }
    submitting.value = true
    try {
      return await submitLoginMFA({
        mfa_pending_token: pendingToken.value,
        code: code.value,
        // Only send the flag when set: the backend treats absent as false and
        // an unasked-for trust grant is a privilege we must not invent.
        ...(trustIP.value ? { trust_ip: true } : {})
      })
    } catch (err) {
      ElMessage.error(loginMFAErrorMessage(err))
      // The code is single-use either way (a TOTP step is spent, a recovery
      // code may have been burned): clear it so nobody retries the same value.
      code.value = ''
      if (isLoginMFASessionLost(err)) reset()
      return null
    } finally {
      submitting.value = false
    }
  }

  return {
    pendingToken,
    code,
    mode,
    trustIP,
    submitting,
    active,
    codeComplete,
    setCode,
    start,
    reset,
    switchMode,
    toggleMode,
    submit
  }
}
