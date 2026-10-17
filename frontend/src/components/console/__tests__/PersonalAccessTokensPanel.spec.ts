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
  proof: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/api/personalAccessTokens', () => ({
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

const item = {
  id: 7,
  name: 'automation',
  scopes: ['profile:read'],
  created_at: 1,
  last_used_at: 0,
  expires_at: 4000000000,
  revoked_at: 0,
}
const token = 'pat_' + 'a'.repeat(48)
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

function render() {
  return mount(PersonalAccessTokensPanel, {
    props: { userId: 1 },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en' })],
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
  mocks.list.mockResolvedValue([{ ...item }])
  mocks.scopes.mockResolvedValue({
    scopes: ['profile:read', 'tokens:read'],
    limit: 20,
    default_lifetime_days: 30,
  })
  mocks.proof.mockResolvedValue('proof')
  mocks.create.mockResolvedValue({
    token,
    item: { ...item, id: 8, name: 'new' },
  })
  mocks.update.mockImplementation((id, input) =>
    Promise.resolve({ ...item, id, ...input })
  )
  mocks.revoke.mockResolvedValue(undefined)
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
      .get('input[type="checkbox"][value="tokens:read"]')
      .setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.proof).toHaveBeenCalledWith({
      scope: 'pat.update',
      context: { id: 7, scopes: ['profile:read', 'tokens:read'] },
    })
    expect(mocks.update).toHaveBeenCalledWith(
      7,
      { name: 'automation', scopes: ['profile:read', 'tokens:read'] },
      'proof',
      expect.any(AbortSignal)
    )
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
      { name: 'new', scopes: ['profile:read'], expires_at: 0 },
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
    mocks.list.mockResolvedValue([{ ...item, revoked_at: 2 }])
    await wrapper.get('[data-testid="confirm-revoke"]').trigger('click')
    await flushPromises()
    expect(mocks.proof).toHaveBeenCalledWith({
      scope: 'pat.revoke',
      context: { id: 7 },
    })
    expect(mocks.revoke).toHaveBeenCalledWith(
      7,
      'proof',
      expect.any(AbortSignal)
    )
    expect(wrapper.text()).toContain('Revoked')
    wrapper.unmount()
  })

  it('disables creation at the returned active limit and reports load failures', async () => {
    mocks.scopes.mockResolvedValue({
      scopes: ['profile:read'],
      limit: 1,
      default_lifetime_days: 30,
    })
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
