// Login captcha state machine (ticket 04).
//
// Lifecycle, driven entirely by the backend - there is no probe endpoint:
//   1. page load: dormant. No challenge, no request, nothing rendered.
//   2. a login 401 carries captcha_required:true -> activate(): fetch the first
//      challenge and the block appears.
//   3. every later submit carries captcha_id / captcha_answer via payload().
//   4. any further failed login while still required -> a NEW challenge is
//      fetched and the answer box is cleared (a consumed/expired challenge id
//      can never be reused).
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { issueCaptcha } from '@/api/auth'

export function useLoginCaptcha() {
  // challengeId empty means "no live challenge" (dormant, or a fetch failed).
  const challengeId = ref('')
  // imageSrc is the data URI returned by the backend, rendered as-is.
  const imageSrc = ref('')
  const answer = ref('')
  // required latches once the backend has flagged this IP; it stays on so a
  // failed image fetch does not silently drop the field from the form.
  const required = ref(false)
  const refreshing = ref(false)

  // visible drives the template: the block only exists once required.
  const visible = computed(() => required.value)

  // fetchChallenge replaces the current challenge, clearing any stale answer.
  // Errors are reported but never thrown: a missing image must not break the
  // login form, the user can retry with the refresh button.
  async function fetchChallenge(): Promise<void> {
    if (refreshing.value) return
    refreshing.value = true
    answer.value = ''
    try {
      const ch = await issueCaptcha()
      challengeId.value = ch.challenge_id
      imageSrc.value = ch.image_base64
    } catch (err) {
      challengeId.value = ''
      imageSrc.value = ''
      const status = (err as { response?: { status?: number } } | null)?.response?.status
      ElMessage.error(
        status === 429
          ? '验证码获取过于频繁，请稍后点击「换一张」'
          : '验证码加载失败，请点击「换一张」'
      )
    } finally {
      refreshing.value = false
    }
  }

  // handleFailure runs after every failed login attempt. stillRequired is the
  // captcha_required flag off the 401 body. Semantics:
  // - flag set: this IP needs a captcha from now on. Fetch a fresh challenge -
  //   activation on first sight, replacement afterwards (the previous id is
  //   spent or wrong, it can never be reused).
  // - flag clear while already required: keep the field (the backend may still
  //   demand one on the next try) but do not spend a new issue quota.
  async function handleFailure(stillRequired: boolean): Promise<void> {
    if (!stillRequired) return
    required.value = true
    await fetchChallenge()
  }

  // payload returns the captcha fields to merge into the login body, or an
  // empty object while dormant so the happy path sends exactly what it used to.
  function payload(): { captcha_id?: string; captcha_answer?: string } {
    if (!required.value || !challengeId.value) return {}
    return { captcha_id: challengeId.value, captcha_answer: answer.value }
  }

  return {
    challengeId,
    imageSrc,
    answer,
    required,
    refreshing,
    visible,
    fetchChallenge,
    handleFailure,
    payload
  }
}
