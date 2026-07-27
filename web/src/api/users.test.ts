// Tests for the admin user-management API wrapper: path construction,
// request payloads and response pass-through.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import client from '@/api/client'
import {
  listUsers,
  getUser,
  createUser,
  updateUser,
  disableUser,
  enableUser,
  deleteUser,
  resetUserPassword
} from '@/api/users'

vi.mock('@/api/client')

describe('admin users API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('listUsers requests /admin/users and returns the bare array', async () => {
    vi.mocked(client.get).mockResolvedValue([] as never)
    const res = await listUsers()
    expect(client.get).toHaveBeenCalledWith('/admin/users')
    expect(res).toEqual([])
  })

  it('getUser requests /admin/users/{id}', async () => {
    vi.mocked(client.get).mockResolvedValue({} as never)
    await getUser(7)
    expect(client.get).toHaveBeenCalledWith('/admin/users/7')
  })

  it('createUser posts username/password with flat quota fields', async () => {
    vi.mocked(client.post).mockResolvedValue({} as never)
    const payload = {
      username: 'alice',
      password: 'p@ss',
      max_airports: 5,
      max_endpoints: 10,
      xray_port_start: 20000,
      xray_port_end: 20010
    }
    await createUser(payload)
    // skipErrorToast: the view localizes create failures itself
    expect(client.post).toHaveBeenCalledWith('/admin/users', payload, { skipErrorToast: true })
  })

  it('updateUser puts flat quota/role payload', async () => {
    vi.mocked(client.put).mockResolvedValue({} as never)
    const payload = {
      role: 'user',
      max_airports: 3,
      max_endpoints: 6,
      xray_port_start: 21000,
      xray_port_end: 21010
    }
    await updateUser(7, payload)
    expect(client.put).toHaveBeenCalledWith('/admin/users/7', payload)
  })

  it('disableUser posts to /admin/users/{id}/disable', async () => {
    vi.mocked(client.post).mockResolvedValue({} as never)
    await disableUser(7)
    expect(client.post).toHaveBeenCalledWith('/admin/users/7/disable')
  })

  it('enableUser posts to /admin/users/{id}/enable', async () => {
    vi.mocked(client.post).mockResolvedValue({} as never)
    await enableUser(7)
    expect(client.post).toHaveBeenCalledWith('/admin/users/7/enable')
  })

  it('deleteUser deletes /admin/users/{id}', async () => {
    vi.mocked(client.delete).mockResolvedValue({} as never)
    await deleteUser(7)
    expect(client.delete).toHaveBeenCalledWith('/admin/users/7')
  })

  it('resetUserPassword posts and returns the new password', async () => {
    vi.mocked(client.post).mockResolvedValue({
      ok: true,
      password: 'abc123',
      user_id: 7,
      username: 'alice'
    } as never)
    const res = await resetUserPassword(7)
    expect(client.post).toHaveBeenCalledWith('/admin/users/7/reset-password')
    expect(res.password).toBe('abc123')
  })
})
