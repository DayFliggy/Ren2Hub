import { beforeEach, describe, expect, it, vi } from 'vitest'
const client = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('../client', () => ({ api: client }))
import { accountSecurityApi } from '../accountSecurity'
beforeEach(() => vi.resetAllMocks())
describe('account security domain contracts', () => {
  it('only accepts recovery codes returned by regeneration', async () => {
    client.post.mockResolvedValueOnce({})
    expect(
      await accountSecurityApi.changeTwoFactor('disable', 'proof')
    ).toEqual({ action: 'disable' })
    client.post.mockResolvedValueOnce({})
    await expect(
      accountSecurityApi.changeTwoFactor('regenerate', 'proof')
    ).rejects.toThrow()
    client.post.mockResolvedValueOnce({ backup_codes: ['one-use-code'] })
    expect(
      await accountSecurityApi.changeTwoFactor('regenerate', 'proof')
    ).toEqual({ action: 'regenerate', backup_codes: ['one-use-code'] })
  })
  it('distinguishes disabled 2FA and an unavailable recovery-code count', async () => {
    client.get.mockResolvedValueOnce({ enabled: false, locked: false })
    expect(await accountSecurityApi.twoFactorStatus()).toEqual({
      enabled: false,
      backup_codes_remaining: null,
    })
    client.get.mockResolvedValueOnce({ enabled: true })
    expect(
      (await accountSecurityApi.twoFactorStatus()).backup_codes_remaining
    ).toBeNull()
    client.get.mockResolvedValueOnce({
      enabled: true,
      backup_codes_remaining: 0,
    })
    expect(
      (await accountSecurityApi.twoFactorStatus()).backup_codes_remaining
    ).toBe(0)
  })
  it('rejects malformed security statuses and dates', async () => {
    client.get.mockResolvedValueOnce({ enabled: 'false' })
    await expect(accountSecurityApi.passkeyStatus()).rejects.toThrow(
      'Invalid API response'
    )
    client.get.mockResolvedValueOnce({
      enabled: true,
      last_used_at: 'not-a-date',
    })
    await expect(accountSecurityApi.passkeyStatus()).rejects.toThrow()
    client.get.mockResolvedValueOnce({
      enabled: true,
      backup_codes_remaining: '8',
    })
    await expect(accountSecurityApi.twoFactorStatus()).rejects.toThrow()
  })
  it('requires complete enrollment data before showing recovery codes', async () => {
    client.post.mockResolvedValueOnce({
      flow_token: 'flow',
      qr_code_data: 'qr',
    })
    await expect(accountSecurityApi.setupTwoFactor('proof')).rejects.toThrow()
    expect(client.post).toHaveBeenCalledWith('/api/user/2fa/setup', undefined, {
      headers: { 'X-Security-Proof': 'proof' },
    })
  })
  it('preserves email enrollment and resends using the existing flow', async () => {
    const flow = {
      flow_token: 'flow',
      old_email_required: true,
      resend_at: 100,
    }
    client.post.mockResolvedValue(flow)
    expect(
      await accountSecurityApi.startEmailBinding('new@example.test', 'proof')
    ).toEqual(flow)
    expect(await accountSecurityApi.resendEmailBinding('flow')).toEqual(flow)
    expect(client.post).toHaveBeenLastCalledWith(
      '/api/oauth/email/bind/resend',
      { flow_token: 'flow' }
    )
  })
  it('does not turn a missing binding list into an empty successful list', async () => {
    client.get.mockResolvedValueOnce(undefined)
    await expect(accountSecurityApi.oauthBindings()).rejects.toThrow()
    client.get.mockResolvedValueOnce([])
    expect(await accountSecurityApi.oauthBindings()).toEqual([])
  })
})
