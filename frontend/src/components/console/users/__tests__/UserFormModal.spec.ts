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
    props: { open: true, editing, operator: { id: 1, level: 100 }, save },
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
  remark: 'VIP contact',
  role: 1,
  status: 2,
  max_concurrency: 0,
} as unknown as AdminUser

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
        max_concurrency: 0,
        password: 'test-password',
        role,
      })
      expect(wrapper.emitted('close')).toEqual([[]])
      wrapper.unmount()
    }
  )

  it('shows the bound email read-only and never sends it', async () => {
    const { wrapper, save, submit } = render(existingUser)
    const email = wrapper.get<HTMLInputElement>('[name="admin-user-email"]')
    expect(email.element.value).toBe('existing@example.com')
    expect(email.element.readOnly).toBe(true)
    await wrapper
      .get('[name="admin-user-display-name"]')
      .setValue('Renamed user')
    await submit()

    expect(save).toHaveBeenCalledExactlyOnceWith({
      username: 'existing-user',
      display_name: 'Renamed user',
      remark: 'VIP contact',
      max_concurrency: 0,
      role: 1,
    })
    wrapper.unmount()
  })

  it('sends the edited remark and preserves the draft when saving fails', async () => {
    const { wrapper, save, submit } = render(existingUser)
    save.mockResolvedValueOnce(false)
    await wrapper.get('[name="admin-user-remark"]').setValue('Renewed contract')
    await submit()

    const expected = {
      username: 'existing-user',
      display_name: 'Existing user',
      remark: 'Renewed contract',
      max_concurrency: 0,
      role: 1,
    }
    expect(save).toHaveBeenCalledExactlyOnceWith(expected)
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(
      wrapper.get<HTMLTextAreaElement>('[name="admin-user-remark"]').element
        .value
    ).toBe('Renewed contract')
    await submit()
    expect(save).toHaveBeenLastCalledWith(expected)
    expect(wrapper.emitted('close')).toEqual([[]])
    wrapper.unmount()
  })

  it('limits remark to 255 characters counted like upstream runes', async () => {
    const { wrapper, save, submit } = render(existingUser)
    const remark = wrapper.get('[name="admin-user-remark"]')
    await remark.setValue('😀'.repeat(255))
    await submit()
    expect(save).toHaveBeenCalledOnce()

    await remark.setValue('a'.repeat(256))
    await submit()
    expect(save).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('creates users with any trimmed username of 1-20 runes like upstream', async () => {
    const { wrapper, save, submit } = render()
    const username = wrapper.get('[name="admin-user-username"]')
    await wrapper.get('[name="admin-user-password"]').setValue('test-password')
    for (const rejected of ['   ', 'a'.repeat(21), '用'.repeat(21)]) {
      await username.setValue(rejected)
      await submit()
    }
    expect(save).not.toHaveBeenCalled()

    await username.setValue('  张三  ')
    await submit()
    expect(save).toHaveBeenLastCalledWith(
      expect.objectContaining({ username: '张三' })
    )
    wrapper.unmount()
  })

  it.each(['ab', '😀'.repeat(20), 'ops admin'])(
    'saves an existing user whose native username is %s',
    async (name) => {
      const { wrapper, save, submit } = render({
        ...existingUser,
        username: name,
      })
      await submit()
      expect(save).toHaveBeenCalledExactlyOnceWith(
        expect.objectContaining({ username: name })
      )
      wrapper.unmount()
    }
  )

  it('keeps an edge U+FEFF in an existing username when only the remark changes', async () => {
    // Go strings.TrimSpace keeps U+FEFF; String.prototype.trim would drop it
    // and silently rename the user.
    const username = '\uFEFFmember\uFEFF'
    const { wrapper, save, submit } = render({ ...existingUser, username })
    await wrapper.get('[name="admin-user-remark"]').setValue('Renewed contract')
    await submit()
    expect(save).toHaveBeenCalledExactlyOnceWith(
      expect.objectContaining({ username, remark: 'Renewed contract' })
    )
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
      max_concurrency: 0,
      password: 'test-password',
      role: 1,
    })
    wrapper.unmount()
  })
})
