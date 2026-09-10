import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { accountClassesApi } from '@/api/accountClasses'
import AccountClassesPanel from './AccountClassesPanel.vue'
import en from '@/i18n/locales/en/console'

const state = vi.hoisted(() => ({
  auth: { isRoot: true },
  success: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => state.auth }))
vi.mock('@/composables/useToast', () => ({ useToast: () => state }))

const row = {
  code: 'default',
  name: 'Default',
  default_concurrency: 5,
  default_rpm: 0,
  topup_multiplier: 1,
  policy_version: 7,
}

function setup() {
  return mount(AccountClassesPanel, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: {
            en: { ...en, common: { cancel: 'Cancel', save: 'Save' } },
          },
        }),
      ],
      stubs: {
        ConsoleModal: {
          props: ['open'],
          template: '<div v-if="open"><slot /><slot name="footer" /></div>',
        },
      },
    },
  })
}

describe('account classes administration', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
    state.auth.isRoot = true
    vi.spyOn(accountClassesApi, 'list').mockResolvedValue([row])
    vi.spyOn(accountClassesApi, 'update').mockResolvedValue(undefined)
    vi.spyOn(accountClassesApi, 'create').mockResolvedValue(undefined)
  })

  it('shows the directory without write controls for a non-root administrator', async () => {
    state.auth.isRoot = false
    const wrapper = setup()
    await flushPromises()
    expect(wrapper.text()).toContain('Default')
    expect(wrapper.find('[aria-label="Edit account class"]').exists()).toBe(
      false
    )
    expect(
      wrapper
        .findAll('button')
        .some((button) => button.text().includes('Create account class'))
    ).toBe(false)
    wrapper.unmount()
  })

  it('keeps codes immutable and sends the loaded policy version with explicit zero', async () => {
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[aria-label="Edit account class"]').trigger('click')
    expect(
      wrapper.get('input[name="account-class-code"]').attributes('readonly')
    ).toBeDefined()
    await wrapper.get('input[name="account-class-concurrency"]').setValue('0')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save')!
      .trigger('click')
    await flushPromises()
    expect(accountClassesApi.update).toHaveBeenCalledWith({
      ...row,
      default_concurrency: 0,
    })
    expect(accountClassesApi.create).not.toHaveBeenCalled()
    expect(
      wrapper.get('[aria-label="Delete account class"]').attributes('disabled')
    ).toBeDefined()
    wrapper.unmount()
  })

  it('rejects zero recharge multipliers and preserves edits on a version conflict', async () => {
    vi.mocked(accountClassesApi.update).mockRejectedValue(
      new Error('account class version conflict')
    )
    const wrapper = setup()
    await flushPromises()
    await wrapper.get('[aria-label="Edit account class"]').trigger('click')
    const input = wrapper.get('input[name="account-class-multiplier"]')
    const save = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save')!
    await input.setValue('0')
    expect(save.attributes('disabled')).toBeDefined()
    await input.setValue('1.25')
    await save.trigger('click')
    await flushPromises()
    expect(state.error).toHaveBeenCalledWith('account class version conflict')
    expect(
      wrapper.get('input[name="account-class-multiplier"]').element
    ).toHaveProperty('value', '1.25')
    wrapper.unmount()
  })
})
