import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { accountClassesApi } from '@/api/accountClasses'
import UserFormModal from './UserFormModal.vue'
import type { AdminUser } from '@/types/console'
import en from '@/i18n/locales/en/console'

const user: AdminUser = {
  id: 2,
  username: 'test.user',
  display_name: 'Test',
  email: '',
  role: 1,
  status: 1,
  quota: 50,
  used_quota: 0,
  request_count: 0,
  invited_count: 0,
  affiliate_quota: 0,
  inviter_id: 0,
  created_time: 0,
  last_login_time: 0,
  account_class: 'default',
  concurrency_override: 0,
  rpm_override: null,
  effective_concurrency: 0,
  effective_rpm: 0,
  policy_version: 8,
}

function setup() {
  const save = vi.fn().mockResolvedValue(true)
  const wrapper = mount(UserFormModal, {
    props: { open: true, editing: user, operatorLevel: 10, save },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: { en: { ...en, common: { cancel: 'Cancel' } } },
        }),
      ],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
  return { wrapper, save }
}

describe('user account class and limits form', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(accountClassesApi, 'list').mockResolvedValue([
      {
        code: 'default',
        name: 'Default',
        default_concurrency: 5,
        default_rpm: 0,
        topup_multiplier: 1,
        policy_version: 2,
      },
    ])
  })

  it('submits explicit zero, inherited RPM and the loaded policy version', async () => {
    const { wrapper, save } = setup()
    await flushPromises()
    expect(
      wrapper.get('input[name="admin-user-concurrency"]').element
    ).toHaveProperty('value', '0')
    expect(wrapper.get('input[name="admin-user-rpm"]').element).toHaveProperty(
      'value',
      ''
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save user')!
      .trigger('click')
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({
        account_class: 'default',
        concurrency_override: 0,
        rpm_override: null,
        policy_version: 8,
      })
    )
    wrapper.unmount()
  })

  it('rejects negative overrides and preserves inheritance when cleared', async () => {
    const { wrapper, save } = setup()
    await flushPromises()
    const input = wrapper.get('input[name="admin-user-concurrency"]')
    const submit = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save user')!
    await input.setValue('-1')
    expect(submit.attributes('disabled')).toBeDefined()
    await input.setValue('')
    expect(submit.attributes('disabled')).toBeUndefined()
    await submit.trigger('click')
    expect(save).toHaveBeenCalledWith(
      expect.objectContaining({ concurrency_override: null })
    )
    wrapper.unmount()
  })

  it('blocks writes when the class directory fails and exposes retry', async () => {
    vi.mocked(accountClassesApi.list).mockRejectedValueOnce(
      new Error('Directory unavailable')
    )
    const { wrapper, save } = setup()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('Directory unavailable')
    expect(
      wrapper
        .findAll('button')
        .find((button) => button.text() === 'Save user')!
        .attributes('disabled')
    ).toBeDefined()
    expect(save).not.toHaveBeenCalled()
    await wrapper
      .get('input[name="admin-user-display-name"]')
      .setValue('Edited Name')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Retry')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(
      wrapper.get('input[name="admin-user-display-name"]').element
    ).toHaveProperty('value', 'Edited Name')
    wrapper.unmount()
  })
})
