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
  it('parses the native 2FA setup and requires every enrollment field', async () => {
    const setup = {
      secret: 'JBSWY3DPEHPK3PXP',
      qr_code_data:
        'otpauth://totp/Ren2Hub:demo?secret=JBSWY3DPEHPK3PXP&issuer=Ren2Hub',
      backup_codes: ['AAAA-1111', 'BBBB-2222', 'CCCC-3333', 'DDDD-4444'],
      flow_token: 'flow',
      expires_at: 1_800_000_600,
    }
    client.post.mockResolvedValueOnce(setup)
    await expect(accountSecurityApi.setupTwoFactor('proof')).resolves.toEqual(
      setup
    )
    expect(client.post).toHaveBeenCalledWith('/api/user/2fa/setup', undefined, {
      headers: { 'X-Security-Proof': 'proof' },
    })

    for (const field of ['secret', 'expires_at'] as const) {
      const { [field]: _omitted, ...incomplete } = setup
      client.post.mockResolvedValueOnce(incomplete)
      await expect(
        accountSecurityApi.setupTwoFactor('proof')
      ).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
    }
    client.post.mockResolvedValueOnce({ ...setup, expires_at: '1800000600' })
    await expect(accountSecurityApi.setupTwoFactor('proof')).rejects.toThrow()
  })
  it('preserves email enrollment and resends using the existing flow', async () => {
    // service.EmailBindingData as unwrapped by the client.
    const flow = {
      flow_token: 'flow',
      email: 'new@example.test',
      current_email: 'old@example.test',
      old_email_required: true,
      expires_at: 1_800_000_600,
      resend_at: 100,
      notification_warning: true,
    }
    const parsed = {
      flow_token: 'flow',
      old_email_required: true,
      resend_at: 100,
      notification_warning: true,
    }
    client.post.mockResolvedValue(flow)
    expect(
      await accountSecurityApi.startEmailBinding('new@example.test', 'proof')
    ).toEqual(parsed)
    client.post.mockResolvedValue({ ...flow, notification_warning: false })
    expect(await accountSecurityApi.resendEmailBinding('flow')).toEqual({
      ...parsed,
      notification_warning: false,
    })
    expect(client.post).toHaveBeenLastCalledWith(
      '/api/oauth/email/bind/resend',
      { flow_token: 'flow' }
    )
    const { notification_warning: _, ...unflagged } = flow
    client.post.mockResolvedValue(unflagged)
    await expect(
      accountSecurityApi.resendEmailBinding('flow')
    ).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
  })
  it('returns the notification warning of every binding change', async () => {
    client.post.mockResolvedValueOnce({ notification_warning: true })
    expect(await accountSecurityApi.bindEmail('flow', '123456', '')).toEqual({
      notification_warning: true,
    })
    client.post.mockResolvedValueOnce({ notification_warning: false })
    expect(await accountSecurityApi.bindWechat('wx-code', 'proof')).toEqual({
      notification_warning: false,
    })
    client.delete.mockResolvedValueOnce({ notification_warning: true })
    expect(await accountSecurityApi.unbindOauth(7, 'proof')).toEqual({
      notification_warning: true,
    })
    expect(client.delete).toHaveBeenCalledWith(
      '/api/user/oauth/bindings/7',
      undefined,
      { headers: { 'X-Security-Proof': 'proof' } }
    )
  })
  it('rejects a binding change without the notification flag', async () => {
    client.post.mockResolvedValueOnce({})
    await expect(
      accountSecurityApi.bindEmail('flow', '123456', '')
    ).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
    client.post.mockResolvedValueOnce({ notification_warning: 'false' })
    await expect(
      accountSecurityApi.bindWechat('wx-code', 'proof')
    ).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
    client.delete.mockResolvedValueOnce(undefined)
    await expect(
      accountSecurityApi.unbindOauth(7, 'proof')
    ).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
  })
  it('does not turn a missing binding list into an empty successful list', async () => {
    client.get.mockResolvedValueOnce(undefined)
    await expect(accountSecurityApi.oauthBindings()).rejects.toThrow()
    client.get.mockResolvedValueOnce([])
    expect(await accountSecurityApi.oauthBindings()).toEqual([])
  })
})
