// Admin user-management API wrapper (ticket 05, contract aligned with ticket 03).
// All endpoints require the super_admin role; the backend enforces 403 otherwise.
//
// Wire contract (see internal/server/handlers_users.go adminUserView):
// - GET /admin/users returns a BARE ARRAY, not an envelope.
// - Usage counts are top-level (airport_count / endpoint_count), not on quota.
// - quota is null when the user has no quota row (e.g. the seeded super admin).
import client from './client'

// UserQuota describes per-user resource limits configured by the admin.
export interface UserQuota {
  max_airports: number
  max_endpoints: number
  xray_port_start: number
  xray_port_end: number
}

// AdminUser is a single row of GET /api/admin/users (backend adminUserView).
export interface AdminUser {
  id: number
  username: string
  role: string
  disabled: boolean
  must_change_password: boolean
  created_at: string
  last_login_at?: string
  quota: UserQuota | null
  airport_count: number
  endpoint_count: number
}

// CreateUserPayload is the body of POST /api/admin/users. Field names match
// the backend adminCreateUserRequest: quota fields are FLAT, not nested.
export interface CreateUserPayload {
  username: string
  password: string
  max_airports: number
  max_endpoints: number
  xray_port_start: number
  xray_port_end: number
}

// UpdateUserPayload is the body of PUT /api/admin/users/{id}. Every field is
// optional; omitted fields are left unchanged server-side.
export interface UpdateUserPayload {
  role?: string
  max_airports?: number
  max_endpoints?: number
  xray_port_start?: number
  xray_port_end?: number
}

// ResetPasswordResponse carries the server-generated new password (field
// name is `password`, matching the backend handler).
export interface ResetPasswordResponse {
  ok: boolean
  password: string
  user_id: number
  username: string
}

// listUsers fetches all users with quota and usage counts.
export function listUsers(): Promise<AdminUser[]> {
  return client.get<unknown, AdminUser[]>('/admin/users')
}

// getUser fetches a single user's detail.
export function getUser(id: number): Promise<AdminUser> {
  return client.get<unknown, AdminUser>(`/admin/users/${id}`)
}

// createUser creates a new user with an initial password and quota.
// skipErrorToast: the view maps backend reasons (reserved/taken username)
// onto localized messages, so the interceptor's generic toast is suppressed.
export function createUser(payload: CreateUserPayload): Promise<AdminUser> {
  return client.post<unknown, AdminUser>('/admin/users', payload, { skipErrorToast: true })
}

// updateUser updates quota and/or role of an existing user.
export function updateUser(id: number, payload: UpdateUserPayload): Promise<AdminUser> {
  return client.put<unknown, AdminUser>(`/admin/users/${id}`, payload)
}

// disableUser soft-disables a user (login rejected, resources kept).
export function disableUser(id: number): Promise<{ ok: boolean; disabled: boolean }> {
  return client.post<unknown, { ok: boolean; disabled: boolean }>(`/admin/users/${id}/disable`)
}

// enableUser re-enables a previously disabled user.
export function enableUser(id: number): Promise<{ ok: boolean; disabled: boolean }> {
  return client.post<unknown, { ok: boolean; disabled: boolean }>(`/admin/users/${id}/enable`)
}

// deleteUser physically deletes a user and cascades resource cleanup.
export function deleteUser(id: number): Promise<{ ok: boolean }> {
  return client.delete<unknown, { ok: boolean }>(`/admin/users/${id}`)
}

// resetUserPassword resets the password to a server-generated one and
// marks the account as must_change_password.
export function resetUserPassword(id: number): Promise<ResetPasswordResponse> {
  return client.post<unknown, ResetPasswordResponse>(`/admin/users/${id}/reset-password`)
}

// CurrentViewResponse mirrors GET /api/admin/current-view: the effective
// UserScope plus the profile being viewed (self when acting=false, the
// impersonated user when acting=true).
export interface CurrentViewResponse {
  user_id: number
  username: string
  role: string
  acting_user_id: number
  acting_username?: string
  acting: boolean
  profile: AdminUser
}

// switchUser asks the backend to enter the target user's space (ticket 09).
// The session's acting_user_id is persisted server-side; subsequent API
// calls automatically resolve to the target's resources.
export function switchUser(userId: number): Promise<AdminUser> {
  return client.post<unknown, AdminUser>('/admin/switch-user', { user_id: userId })
}

// exitSwitch clears the session's acting_user_id; idempotent.
export function exitSwitch(): Promise<{ ok: boolean }> {
  return client.post<unknown, { ok: boolean }>('/admin/exit-switch')
}

// currentView reports the caller's effective scope so the navbar can show
// "viewing as X" when impersonating.
export function currentView(): Promise<CurrentViewResponse> {
  return client.get<unknown, CurrentViewResponse>('/admin/current-view')
}
