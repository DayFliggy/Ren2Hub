import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import UserFormModal from '../UserFormModal.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { AdminUser } from '@/types/console'

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isRoot: true }),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn() }),
}))

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

function render(editing: AdminUser | null = null) {
  const save = vi.fn().mockResolvedValue(true)
  const wrapper = mount(UserFormModal, {
    props: { open: true, editing, operatorLevel: 100, save },
    global: {
      plugins: [i18n],
      stubs: {
        ConsoleModal: {
          props: ['open'],
          template: '<div v-if="open"><slot/><slot name="footer"/></div>',
        },
        AdminUserBindings: true,
      },
    },
  })
  const submit = async () => {
    await wrapper
      .findAll('button')
      .find((button) => button.text() === i18n.global.t('users.saveUser'))!
      .trigger('click')
    await flushPromises()
  }
  return { wrapper, save, submit }
}

const existingUser = {
  id: 42,
  username: 'existing-user',
  display_name: 'Existing user',
  email: 'existing@example.com',
  role: 1,
  status: 2,
  group: 'vip',
} as AdminUser

describe('native user form contract', () => {
  it.each([1, 10])(
    'creates role %s with only supported fields and no ignored controls',
    async (role) => {
      const { wrapper, save, submit } = render()
      expect(wrapper.find('[name="admin-user-email"]').exists()).toBe(false)
      expect(wrapper.find('[name="admin-user-group"]').exists()).toBe(false)
      expect(wrapper.find('[role="radiogroup"]').exists()).toBe(false)

      await wrapper.get('[name="admin-user-username"]').setValue('new-user')
      await wrapper.get('[name="admin-user-display-name"]').setValue('New user')
      await wrapper
        .get('[name="admin-user-password"]')
        .setValue('test-password')
      wrapper
        .getComponent(FilterSelect)
        .vm.$emit('update:modelValue', String(role))
      await flushPromises()
      await submit()

      expect(save).toHaveBeenCalledExactlyOnceWith({
        username: 'new-user',
        display_name: 'New user',
        password: 'test-password',
        role,
      })
      expect(wrapper.emitted('close')).toEqual([[]])
      wrapper.unmount()
    }
  )

  it('keeps email and group in edits and preserves the draft when saving fails', async () => {
    const { wrapper, save, submit } = render(existingUser)
    save.mockResolvedValueOnce(false)
    await wrapper
      .get('[name="admin-user-email"]')
      .setValue('updated@example.com')
    await wrapper.get('[name="admin-user-group"]').setValue('premium')
    await submit()

    const expected = {
      username: 'existing-user',
      display_name: 'Existing user',
      email: 'updated@example.com',
      group: 'premium',
      role: 1,
    }
    expect(save).toHaveBeenCalledExactlyOnceWith(expected)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(
      wrapper.get<HTMLInputElement>('[name="admin-user-email"]').element.value
    ).toBe('updated@example.com')
    await submit()
    expect(save).toHaveBeenLastCalledWith(expected)
    expect(wrapper.emitted('close')).toEqual([[]])
    wrapper.unmount()
  })

  it('does not carry an edited account email, group or disabled state into a later creation', async () => {
    const { wrapper, save, submit } = render(existingUser)
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true, editing: null })
    await wrapper.get('[name="admin-user-username"]').setValue('another-user')
    await wrapper.get('[name="admin-user-password"]').setValue('test-password')
    await submit()

    expect(save).toHaveBeenCalledExactlyOnceWith({
      username: 'another-user',
      display_name: '',
      password: 'test-password',
      role: 1,
    })
    wrapper.unmount()
  })
})
