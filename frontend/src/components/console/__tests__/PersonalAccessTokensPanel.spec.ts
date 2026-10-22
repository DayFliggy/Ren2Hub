import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  list: vi.fn(),
  scopes: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  revoke: vi.fn(),
  revokeLegacy: vi.fn(),
  proof: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/api/personalAccessTokens', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  personalAccessTokensApi: mocks,
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ success: mocks.success, error: mocks.error }),
}))
vi.mock('@/components/auth/SecurityProofDialog.vue', () => ({
  default: defineComponent({
    setup(_, { expose }) {
      expose({ request: mocks.proof })
      return () => null
    },
  }),
}))
import PersonalAccessTokensPanel from '../PersonalAccessTokensPanel.vue'
import { parsePATScopeCatalog } from '@/api/personalAccessTokens'
import { accessTokenCatalogFixture } from '@/api/__tests__/accessTokenCatalogFixture'
import { loadServerMessages } from '@/i18n/serverMessages'

const item = {
  id: 7,
  name: 'automation',
  scopes: ['profile:read'],
  created_at: 1,
  last_used_at: 0,
  expires_at: 4000000000,
  token_ref: 'ref',
  token_hint: 'nap_****',
  last_used_ip: '',
}
// ListAccessTokens legacy entry; *int64 times are null without a record.
const legacy = {
  token_hint: 'k9Zq',
  token_ref: 'legacy-ref',
  created_at: null,
  last_used_at: null,
  last_used_ip: '',
  retire_at: 1_900_000_000,
}
const token = 'nap_' + 'a'.repeat(43)
const modal = {
  props: { open: Boolean },
  emits: ['close'],
  template: '<div v-if="open"><slot /></div>',
}
const confirm = {
  props: { open: Boolean },
  emits: ['confirm', 'cancel'],
  template:
    '<button v-if="open" data-testid="confirm-revoke" @click="$emit(\'confirm\')">Confirm revoke</button>',
}

function render(locale = 'en') {
  return mount(PersonalAccessTokensPanel, {
    props: { userId: 1 },
    global: {
      plugins: [createI18n({ legacy: false, locale })],
      stubs: { ConsoleModal: modal, ConfirmDialog: confirm },
    },
  })
}
function button(wrapper: ReturnType<typeof render>, text: string) {
  const result = wrapper.findAll('button').find((b) => b.text() === text)
  expect(result, text).toBeDefined()
  return result!
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.list.mockResolvedValue({ items: [{ ...item }], legacy: null })
  mocks.scopes.mockResolvedValue(
    parsePATScopeCatalog(accessTokenCatalogFixture())
  )
  mocks.proof.mockResolvedValue('proof')
  mocks.create.mockResolvedValue({
    token,
    item: { ...item, id: 8, name: 'new' },
  })
  mocks.update.mockImplementation((id, input) =>
    Promise.resolve({ ...item, id, ...input })
  )
  mocks.revoke.mockResolvedValue(undefined)
  mocks.revokeLegacy.mockResolvedValue(undefined)
})

describe('PAT management', () => {
  it('renames without proof and leaves permissions absent from the update', async () => {
    const wrapper = render()
    await flushPromises()
    await button(wrapper, 'Rename').trigger('click')
    await wrapper.get('input[aria-label="Name"]').setValue('renamed')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.proof).not.toHaveBeenCalled()
    expect(mocks.update).toHaveBeenCalledWith(
      7,
      { name: 'renamed' },
      undefined,
      expect.any(AbortSignal)
    )
    expect(wrapper.text()).toContain('renamed')
    wrapper.unmount()
  })

  it('binds permission verification to the target and sorted grants', async () => {
    const wrapper = render()
    await flushPromises()
    await button(wrapper, 'Permissions').trigger('click')
    await wrapper
      .get('input[type="checkbox"][value="api_key:read"]')
      .setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.proof).toHaveBeenCalledWith({
      scope: 'access_token.update',
      context: { token_id: 7, scopes: ['api_key:read', 'profile:read'] },
    })
    expect(mocks.update).toHaveBeenCalledWith(
      7,
      { name: 'automation', scopes: ['api_key:read', 'profile:read'] },
      'proof',
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })

  it('renders the catalog by group with zh-CN labels instead of scope keys', async () => {
    await loadServerMessages('zh-CN')
    mocks.list.mockResolvedValue({
      items: [
        {
          ...item,
          scopes: ['ops:read', 'ops:write', 'profile:read', 'wallet:read'],
        },
      ],
      legacy: null,
    })
    const wrapper = render('zh-CN')
    await flushPromises()
    expect(
      wrapper
        .findAll('[data-testid="pat-resources"] span')
        .map((badge) => badge.text())
    ).toEqual(['个人资料', '钱包', '另有 1 项'])
    await button(wrapper, '创建访问令牌').trigger('click')
    const personal = wrapper.get('[data-testid="pat-group-personal"]').text()
    expect(personal).toContain('个人')
    expect(personal).toContain('钱包')
    expect(personal).toContain('查看余额、充值记录和邀请码。')
    const admin = wrapper.get('[data-testid="pat-group-admin"]').text()
    expect(admin).toContain('管理')
    expect(admin).toContain('查看渠道密钥')
    expect(admin).toContain('预留用于在安全验证后查看完整渠道密钥。')
    expect(wrapper.text()).not.toMatch(/wallet|ops|View channel secrets/)
    wrapper.unmount()
  })

  it('lists stored scopes the catalog no longer offers until they are cleared', async () => {
    mocks.list.mockResolvedValue({
      items: [{ ...item, scopes: ['ops:read', 'profile:read'] }],
      legacy: null,
    })
    const wrapper = render()
    await flushPromises()
    await button(wrapper, 'Permissions').trigger('click')
    const stale = wrapper.get('input[type="checkbox"][value="ops:read"]')
    expect(stale.element.closest('section')?.textContent).toContain(
      'No longer grantable'
    )
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.error).toHaveBeenCalledWith(
      'Enter a name, choose at least one available scope, and use a future expiry.'
    )
    expect(mocks.proof).not.toHaveBeenCalled()
    await stale.setValue(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.proof).toHaveBeenCalledWith({
      scope: 'access_token.update',
      context: { token_id: 7, scopes: ['profile:read'] },
    })
    wrapper.unmount()
  })

  it('shows a new secret once and removes it on close', async () => {
    const wrapper = render()
    await flushPromises()
    await button(wrapper, 'Create access token').trigger('click')
    await wrapper.get('input[aria-label="Name"]').setValue('new')
    await wrapper
      .get('input[type="checkbox"][value="profile:read"]')
      .setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(
      { name: 'new', scopes: ['profile:read'], expires_at: expect.any(Number) },
      'proof',
      expect.any(AbortSignal)
    )
    expect(wrapper.get('[data-testid="pat-secret"]').text()).toBe(token)
    await button(wrapper, 'Done').trigger('click')
    expect(wrapper.text()).not.toContain(token)
    await button(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    expect(wrapper.text()).not.toContain(token)
    wrapper.unmount()
  })

  it('creates a token whose expiry keeps the seconds that were picked', async () => {
    const wrapper = render()
    await flushPromises()
    await button(wrapper, 'Create access token').trigger('click')
    await wrapper.get('input[aria-label="Name"]').setValue('new')
    await wrapper
      .get('input[type="checkbox"][value="profile:read"]')
      .setValue(true)
    await wrapper
      .get('input[type="datetime-local"]')
      .setValue('2099-10-11T08:30:45')
    // requestSubmit runs the browser's constraint validation first.
    wrapper.get<HTMLFormElement>('form').element.requestSubmit()
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(
      {
        name: 'new',
        scopes: ['profile:read'],
        expires_at: new Date(2099, 9, 11, 8, 30, 45).getTime() / 1000,
      },
      'proof',
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })

  it('does not reveal a creation result received after account changes', async () => {
    let resolve!: (value: unknown) => void
    mocks.create.mockReturnValue(
      new Promise((done) => {
        resolve = done
      })
    )
    const wrapper = render()
    await flushPromises()
    await button(wrapper, 'Create access token').trigger('click')
    await wrapper.get('input[aria-label="Name"]').setValue('new')
    await wrapper
      .get('input[type="checkbox"][value="profile:read"]')
      .setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.setProps({ userId: 0 })
    resolve({ token, item })
    await flushPromises()
    expect(wrapper.text()).not.toContain(token)
    expect(wrapper.find('[data-testid="pat-secret"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('requires target-bound verification for revocation and reloads server state', async () => {
    const wrapper = render()
    await flushPromises()
    await button(wrapper, 'Revoke').trigger('click')
    mocks.list.mockResolvedValue({ items: [], legacy: null })
    await wrapper.get('[data-testid="confirm-revoke"]').trigger('click')
    await flushPromises()
    expect(mocks.proof).toHaveBeenCalledWith({
      scope: 'access_token.revoke',
      context: { token_id: 7 },
    })
    expect(mocks.revoke).toHaveBeenCalledWith(
      7,
      'proof',
      expect.any(AbortSignal)
    )
    expect(wrapper.text()).not.toContain('automation')
    wrapper.unmount()
  })

  it('lists the legacy token and revokes it with a legacy-bound proof', async () => {
    mocks.list.mockResolvedValue({ items: [{ ...item }], legacy })
    const wrapper = render()
    await flushPromises()
    const row = wrapper.get('[data-testid="pat-legacy"]')
    expect(row.text()).toContain('Legacy token')
    expect(row.text()).toContain('…k9Zq')
    expect(row.text()).toContain('Never')
    expect(row.text()).not.toContain('Created')
    expect(row.text()).toContain(
      new Date(legacy.retire_at * 1000).toLocaleString('en')
    )
    expect(wrapper.text()).toContain('1 / 20 active tokens')

    await row.get('button').trigger('click')
    mocks.list.mockResolvedValue({ items: [{ ...item }], legacy: null })
    await wrapper.get('[data-testid="confirm-revoke"]').trigger('click')
    await flushPromises()
    expect(mocks.proof).toHaveBeenCalledWith({
      scope: 'access_token.revoke',
      context: { legacy: true },
    })
    expect(mocks.revokeLegacy).toHaveBeenCalledWith(
      'proof',
      expect.any(AbortSignal)
    )
    expect(mocks.revoke).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="pat-legacy"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('automation')
    wrapper.unmount()
  })

  it('drops a legacy token listed for the previous account', async () => {
    let resolve!: (value: unknown) => void
    mocks.list.mockReturnValue(
      new Promise((done) => {
        resolve = done
      })
    )
    const wrapper = render()
    await flushPromises()
    await wrapper.setProps({ userId: 0 })
    resolve({ items: [{ ...item }], legacy })
    await flushPromises()
    expect(wrapper.find('[data-testid="pat-legacy"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('disables creation at the returned active limit and reports load failures', async () => {
    mocks.scopes.mockResolvedValue(
      parsePATScopeCatalog({ ...accessTokenCatalogFixture(), max_tokens: 1 })
    )
    const wrapper = render()
    await flushPromises()
    expect(
      button(wrapper, 'Create access token').attributes('disabled')
    ).toBeDefined()
    mocks.list.mockRejectedValue(new Error('Unavailable'))
    await button(wrapper, 'Refresh').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Unavailable')
    wrapper.unmount()
  })
})
