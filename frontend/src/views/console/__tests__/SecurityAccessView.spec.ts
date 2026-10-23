import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const apiGet = vi.hoisted(() => vi.fn())
const apiPost = vi.hoisted(() => vi.fn())
const apiPut = vi.hoisted(() => vi.fn())
const apiDelete = vi.hoisted(() => vi.fn())
const publicStatus = vi.hoisted(() => vi.fn())
const requestProof = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
}))

vi.mock('@/components/auth/SecurityProofDialog.vue', () => ({
  default: {
    setup(_: unknown, { expose }: { expose: (value: unknown) => void }) {
      expose({ request: requestProof })
      return () => null
    },
  },
}))

// completeOAuth (api/oauth.ts) talks to the shared client directly.
vi.mock('@/api/client', () => ({
  api: { get: apiGet, post: apiPost, put: apiPut, delete: apiDelete },
  setApiUnauthorizedHandler: vi.fn(),
}))

vi.mock('@/api/console', () => ({
  api: {
    get: apiGet,
    post: apiPost,
    put: apiPut,
    delete: apiDelete,
  },
}))

vi.mock('@/api/public', () => ({
  publicApi: {
    status: publicStatus,
  },
}))

import { QrcodeSvg } from 'qrcode.vue'

import {
  clearAuthBundle,
  getAuthBundle,
  setAuthBundle,
} from '@/api/authSession'
import { ApiError } from '@/api/types'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import { useToast } from '@/composables/useToast'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import SecurityAccessView from '@/views/console/SecurityAccessView.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

function securityGet(url: string): Promise<unknown> {
  if (url === '/api/user/passkey') {
    return Promise.resolve({ enabled: false, last_used_at: null })
  }
  if (url === '/api/user/2fa/status') {
    return Promise.resolve({
      enabled: true,
      locked: false,
      backup_codes_remaining: 6,
    })
  }
  if (url === '/api/user/oauth/bindings') return Promise.resolve([])
  if (url === '/api/user/self/personal-access-tokens') {
    return Promise.resolve([])
  }
  if (url === '/api/user/self/personal-access-tokens/scopes') {
    return Promise.resolve({ scopes: [], limit: 10, default_lifetime_days: 30 })
  }
  return Promise.resolve({})
}

const toast = useToast()

beforeEach(() => {
  setActivePinia(createPinia())
  clearAuthBundle()
  toast.toasts.splice(0)
  routerPush.mockReset()
  requestProof.mockReset().mockResolvedValue('test-proof')
  apiGet.mockReset().mockImplementation(securityGet)
  apiPost.mockReset().mockResolvedValue({ flow_token: 'oauth-flow' })
  apiPut.mockReset().mockResolvedValue({})
  publicStatus.mockReset().mockResolvedValue({
    github_oauth: true,
    github_client_id: 'github-client',
    discord_oauth: false,
    linuxdo_oauth: false,
    oidc_enabled: false,
    wechat_login: false,
    telegram_oauth: false,
    telegram_oauth_configured: false,
    custom_oauth_providers: [],
  })

  const auth = useAuthStore()
  auth.persist({
    id: 1,
    username: 'alice',
    display_name: 'Alice',
    email: 'alice@example.com',
    role: 1,
    status: 1,
    quota: 1000,
    used_quota: 0,
    request_count: 0,
    has_password: true,
    setting: JSON.stringify({ record_ip_log: false }),
  })
})

const sessionBundle = {
  access_token: 'session-token',
  token_type: 'Bearer' as const,
  access_expires_at: 2_000_000_000,
  session: {
    sid: 'session-1',
    current: true,
    login_method: 'password',
    ip: '127.0.0.1',
    user_agent: 'vitest',
    created_at: 1,
    last_active_at: 2,
    expires_at: 2_000_000_000,
  },
  user: {
    id: 1,
    username: 'alice',
    display_name: 'Alice',
    email: 'alice@example.com',
    role: 1,
    status: 1,
    quota: 1000,
    used_quota: 0,
    request_count: 0,
    has_password: true,
  },
}

function renderView() {
  return mount(SecurityAccessView, {
    attachTo: document.body,
    global: {
      plugins: [i18n],
      stubs: {
        LoginSessionsPanel: true,
        PersonalAccessTokensPanel: true,
      },
    },
  })
}

function buttonByText(
  wrapper: ReturnType<typeof renderView>,
  text: string
): HTMLButtonElement | undefined {
  return wrapper
    .findAll('button')
    .map((button) => button.element as HTMLButtonElement)
    .find((button) => button.textContent?.trim() === text)
}

const twoFASetup = {
  secret: 'JBSWY3DPEHPK3PXP',
  qr_code_data:
    'otpauth://totp/Ren2Hub:alice?secret=JBSWY3DPEHPK3PXP&issuer=Ren2Hub',
  backup_codes: ['AAAA-1111', 'BBBB-2222', 'CCCC-3333', 'DDDD-4444'],
  flow_token: 'setup-flow',
  expires_at: 1_800_000_600,
}

async function openTwoFASetupDialog() {
  apiGet.mockImplementation((url: string) =>
    url === '/api/user/2fa/status'
      ? Promise.resolve({ enabled: false, locked: false })
      : securityGet(url)
  )
  apiPost.mockImplementation((url: string) =>
    Promise.resolve(url === '/api/user/2fa/setup' ? twoFASetup : {})
  )
  const wrapper = renderView()
  await flushPromises()
  buttonByText(wrapper, '启用两步验证')?.click()
  await flushPromises()
  return wrapper
}

function stubClipboard(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  })
}

const wechatStatus = {
  github_oauth: false,
  discord_oauth: false,
  linuxdo_oauth: false,
  oidc_enabled: false,
  wechat_login: true,
  telegram_oauth: false,
  telegram_oauth_configured: false,
  custom_oauth_providers: [],
}

async function openWeChatBindDialog(wechatQRCode: string) {
  publicStatus.mockResolvedValue({
    ...wechatStatus,
    wechat_qrcode: wechatQRCode,
  })
  const wrapper = renderView()
  await flushPromises()
  buttonByText(wrapper, '绑定')?.click()
  await flushPromises()
  return {
    wrapper,
    dialog: document.body.querySelector('[role="dialog"]')!,
  }
}

describe('SecurityAccessView', () => {
  it('renders profile, credentials, bindings, passkey and 2fa', async () => {
    const wrapper = renderView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('安全与访问')
    expect(text).toContain('Alice')
    expect(text).toContain('alice')
    expect(text).toContain('••••••••••')
    expect(text).toContain('alice@example.com')
    expect(text).toContain('通行密钥认证')
    expect(text).toContain('剩余备用码')
    wrapper.unmount()
  })

  it('leaves IP logging to account settings instead of writing it here', async () => {
    const wrapper = renderView()
    await flushPromises()

    expect(wrapper.findComponent({ name: 'ConsoleToggle' }).exists()).toBe(
      false
    )
    buttonByText(wrapper, '前往账号设置')?.click()
    expect(routerPush).toHaveBeenCalledWith({ name: 'settings' })
    expect(apiPut).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not request the retired single-token API', async () => {
    const wrapper = renderView()
    await flushPromises()

    expect(
      apiGet.mock.calls.some(([url]) =>
        String(url).startsWith('/api/user/token')
      )
    ).toBe(false)
    wrapper.unmount()
  })

  it('shows an unknown state instead of "disabled" when a status fails to load', async () => {
    apiGet.mockImplementation((url: string) =>
      url === '/api/user/passkey'
        ? Promise.reject(new Error('passkey offline'))
        : securityGet(url)
    )
    const wrapper = renderView()
    await flushPromises()

    expect(wrapper.find('[role="alert"]').text()).toContain('passkey offline')
    expect(wrapper.text()).toContain('未知')
    expect(buttonByText(wrapper, '启用 Passkey')?.disabled).toBe(true)
    // The 2FA status loaded on its own and is still shown.
    expect(wrapper.text()).toContain('剩余备用码')
    wrapper.unmount()
  })

  it('shows the backend 2FA lock state', async () => {
    apiGet.mockImplementation((url: string) =>
      url === '/api/user/2fa/status'
        ? Promise.resolve({
            enabled: true,
            locked: true,
            backup_codes_remaining: 0,
          })
        : securityGet(url)
    )
    const wrapper = renderView()
    await flushPromises()
    expect(wrapper.text()).toContain('已锁定')
    wrapper.unmount()
  })

  it('edits the display name through the profile API', async () => {
    const auth = useAuthStore()
    const updateProfile = vi
      .spyOn(auth, 'updateProfile')
      .mockResolvedValue(undefined)
    const wrapper = renderView()
    await flushPromises()

    const editButtons = wrapper.findAll('button.settings-row-action')
    await editButtons[0]!.trigger('click')
    await flushPromises()
    const input = document.body.querySelector<HTMLInputElement>(
      'input[name="display-name"]'
    )
    expect(input?.value).toBe('Alice')
    input!.value = 'Alice Chen'
    input!.dispatchEvent(new Event('input'))
    await flushPromises()
    Array.from(document.body.querySelectorAll('button'))
      .find((button) => button.textContent?.trim() === '保存资料')
      ?.click()
    await flushPromises()

    expect(updateProfile).toHaveBeenCalledWith({ display_name: 'Alice Chen' })
    wrapper.unmount()
  })

  it('opens the password modal from the credential row', async () => {
    const wrapper = renderView()
    await flushPromises()

    const passwordButton = wrapper
      .findAll('button.settings-row-action')
      .find((button) => button.text() === '修改密码')
    expect(passwordButton).toBeDefined()
    await passwordButton!.trigger('click')
    await flushPromises()

    expect(
      document.body.querySelector('[role="dialog"]')?.textContent
    ).toContain('修改密码')
    wrapper.unmount()
  })

  it('renders the 2FA QR from the otpauth URI and copies the secret', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    stubClipboard(writeText)
    const toast = useToast()
    toast.toasts.splice(0)
    const wrapper = await openTwoFASetupDialog()

    expect(requestProof).toHaveBeenCalledWith({ scope: '2fa.setup' })
    expect(wrapper.findComponent(QrcodeSvg).props('value')).toBe(
      twoFASetup.qr_code_data
    )
    const dialog = document.body.querySelector('[role="dialog"]')!
    expect(dialog.querySelector('svg')).not.toBeNull()
    expect(dialog.querySelector('img[src^="otpauth:"]')).toBeNull()
    expect(
      dialog.querySelector('[data-twofa-secret]')?.textContent?.trim()
    ).toBe(twoFASetup.secret)

    Array.from(dialog.querySelectorAll('button'))
      .find((button) => button.textContent?.trim() === '复制密钥')
      ?.click()
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(twoFASetup.secret)
    expect(toast.toasts.at(-1)).toMatchObject({
      type: 'success',
      message: '密钥已复制',
    })
    wrapper.unmount()
    Reflect.deleteProperty(navigator, 'clipboard')
  })

  it('reports a failed secret copy instead of claiming success', async () => {
    stubClipboard(vi.fn().mockRejectedValue(new Error('denied')))
    const toast = useToast()
    toast.toasts.splice(0)
    const wrapper = await openTwoFASetupDialog()

    Array.from(document.body.querySelectorAll('[role="dialog"] button'))
      .find((button) => button.textContent?.trim() === '复制密钥')
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    expect(toast.toasts.at(-1)?.type).toBe('error')
    expect(toast.toasts.some((item) => item.type === 'success')).toBe(false)
    wrapper.unmount()
    Reflect.deleteProperty(navigator, 'clipboard')
  })

  it('shows the configured WeChat QR and binds with the entered code', async () => {
    apiPost.mockImplementation((url: string) =>
      Promise.resolve(
        url === '/api/oauth/wechat/bind' ? { notification_warning: false } : {}
      )
    )
    const { wrapper, dialog } = await openWeChatBindDialog(
      'https://cdn.example.com/wechat.png'
    )

    const image = dialog.querySelector('[data-wechat-qrcode] img')
    expect(image?.getAttribute('src')).toBe(
      'https://cdn.example.com/wechat.png'
    )
    expect(image?.getAttribute('alt')).toBe('微信公众号二维码')
    expect(dialog.textContent).not.toContain('将在第三方窗口中继续绑定')
    const input = dialog.querySelector<HTMLInputElement>(
      'input[autocomplete="one-time-code"]'
    )!
    input.value = ' wx-code '
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    Array.from(dialog.querySelectorAll('button'))
      .filter((button) => button.textContent?.trim() === '绑定')
      .at(-1)
      ?.click()
    await flushPromises()

    expect(requestProof).toHaveBeenCalledWith({
      scope: 'account.binding.bind',
      context: { provider: 'wechat', code: 'wx-code' },
    })
    expect(apiPost).toHaveBeenCalledWith(
      '/api/oauth/wechat/bind',
      { code: 'wx-code' },
      { headers: { 'X-Security-Proof': 'test-proof' } }
    )
    expect(toast.toasts.map((item) => item.type)).not.toContain('warning')
    wrapper.unmount()
  })

  it('warns about a failed notification email even when the refresh fails', async () => {
    apiPost.mockImplementation((url: string) =>
      url === '/api/oauth/wechat/bind'
        ? Promise.resolve({ notification_warning: true })
        : Promise.reject(new Error('refresh failed'))
    )
    const { wrapper, dialog } = await openWeChatBindDialog(
      'https://cdn.example.com/wechat.png'
    )
    const input = dialog.querySelector<HTMLInputElement>(
      'input[autocomplete="one-time-code"]'
    )!
    input.value = 'wx-code'
    input.dispatchEvent(new Event('input'))
    await flushPromises()
    Array.from(dialog.querySelectorAll('button'))
      .filter((button) => button.textContent?.trim() === '绑定')
      .at(-1)
      ?.click()
    await flushPromises()

    expect(apiPost).toHaveBeenCalledWith('/api/user/auth/refresh')
    expect(toast.toasts.map(({ type, message }) => [type, message])).toEqual([
      ['success', '账户绑定已更新'],
      ['warning', '变更已完成，但通知邮件发送失败。'],
      ['error', 'refresh failed'],
    ])
    wrapper.unmount()
  })

  it('keeps the password rotation and warns before refreshing the profile', async () => {
    const { user: _user, ...rotation } = sessionBundle
    setAuthBundle(sessionBundle)
    apiPut.mockResolvedValue({
      ...rotation,
      access_token: 'rotated-token',
      has_password: true,
      notification_warning: true,
    })
    apiGet.mockImplementation((url: string) =>
      url === '/api/user/self'
        ? Promise.reject(new ApiError('refresh failed', { status: 500 }))
        : securityGet(url)
    )
    const wrapper = renderView()
    await flushPromises()
    buttonByText(wrapper, '修改密码')?.click()
    await flushPromises()
    const dialog = document.body.querySelector('[role="dialog"]')!
    for (const [selector, value] of [
      ['input[autocomplete="current-password"]', 'old-password'],
      ['input[autocomplete="new-password"]', 'new-password-123'],
    ]) {
      const input = dialog.querySelector<HTMLInputElement>(selector)!
      input.value = value
      input.dispatchEvent(new Event('input'))
    }
    await flushPromises()
    Array.from(dialog.querySelectorAll('button'))
      .find((button) => button.textContent?.trim() === '保存')
      ?.click()
    await flushPromises()

    expect(requestProof).toHaveBeenCalledWith({
      scope: 'account.password.change',
    })
    expect(getAuthBundle()?.access_token).toBe('rotated-token')
    expect(Object.keys(getAuthBundle()!)).not.toContain('notification_warning')
    expect(toast.toasts.map(({ type, message }) => [type, message])).toEqual([
      ['success', '密码已更新'],
      ['warning', '变更已完成，但通知邮件发送失败。'],
      ['error', 'refresh failed'],
    ])
    const passwordModal = wrapper
      .findAllComponents(ConsoleModal)
      .find((modal) => modal.props('title') === '修改密码')
    expect(passwordModal?.props('open')).toBe(false)
    wrapper.unmount()
  })

  it('tells a missing WeChat QR apart from a rejected one', async () => {
    const missing = await openWeChatBindDialog('')
    expect(missing.dialog.querySelector('[data-wechat-qrcode] img')).toBeNull()
    expect(missing.dialog.textContent).toContain('管理员尚未配置微信二维码')
    missing.wrapper.unmount()

    const invalid = await openWeChatBindDialog('javascript:alert(1)')
    expect(invalid.dialog.querySelector('[data-wechat-qrcode] img')).toBeNull()
    expect(invalid.dialog.textContent).toContain('微信二维码地址无效')
    expect(invalid.dialog.textContent).not.toContain('管理员尚未配置')
    invalid.wrapper.unmount()
  })

  it('closes its OAuth popup and removes window handlers when unmounted', async () => {
    const removeEventListener = vi.spyOn(window, 'removeEventListener')
    const closePopup = vi.fn()
    const openPopup = vi.spyOn(window, 'open').mockReturnValue({
      close: closePopup,
      location: { replace: vi.fn() },
    } as unknown as Window)
    const wrapper = renderView()
    await flushPromises()

    buttonByText(wrapper, '绑定')?.click()
    await flushPromises()
    Array.from(document.body.querySelectorAll('[role="dialog"] button'))
      .filter((button) => button.textContent?.trim() === '绑定')
      .at(-1)
      ?.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    expect(openPopup).toHaveBeenCalledOnce()

    wrapper.unmount()
    await flushPromises()

    expect(closePopup).toHaveBeenCalled()
    expect(removeEventListener).toHaveBeenCalledWith(
      'message',
      expect.any(Function)
    )
    expect(removeEventListener).toHaveBeenCalledWith(
      'beforeunload',
      expect.any(Function)
    )
    openPopup.mockRestore()
    removeEventListener.mockRestore()
  })
})
