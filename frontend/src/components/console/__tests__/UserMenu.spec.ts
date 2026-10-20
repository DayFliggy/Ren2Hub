import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const routerPush = vi.hoisted(() => vi.fn())

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
}))

import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import UserMenu from '@/components/console/UserMenu.vue'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(() => {
  setActivePinia(createPinia())
  routerPush.mockReset()

  const auth = useAuthStore()
  auth.persist({
    id: 1,
    username: 'tester',
    display_name: 'Tester',
    email: 'tester@example.com',
    role: 1,
    status: 1,
    available_quota: '500000',
    reserved_quota: '0',
    used_quota: '0',
    quota_version: 0,
    request_count: 0,
    created_at: 1000,
  })
})

describe('UserMenu', () => {
  it('navigates to security view when clicking security access menu item', async () => {
    const wrapper = mount(UserMenu, {
      global: {
        plugins: [i18n],
      },
    })
    await flushPromises()

    // 点击头像打开菜单
    const avatarBtn = wrapper.find('button[aria-haspopup="menu"]')
    expect(avatarBtn.exists()).toBe(true)
    await avatarBtn.trigger('click')
    await flushPromises()

    // 找到「安全与访问」菜单项
    const items = wrapper.findAll('[data-user-menu-item]')
    const securityItem = items.find((item) =>
      item.text().includes('安全与访问')
    )
    expect(securityItem).toBeDefined()
    await securityItem?.trigger('click')
    await flushPromises()

    // 断言直接跳转到 security 路由，而不再弹出旧弹窗
    expect(routerPush).toHaveBeenCalledWith({ name: 'security' })
  })

  it('confirms the sign-out with a result message, not the button label', async () => {
    const auth = useAuthStore()
    vi.spyOn(auth, 'logout').mockResolvedValue()
    const toast = useToast()
    toast.toasts.splice(0)

    const wrapper = mount(UserMenu, { global: { plugins: [i18n] } })
    await wrapper.get('button[aria-haspopup="menu"]').trigger('click')
    const logout = wrapper
      .findAll('[data-user-menu-item]')
      .find((item) => item.text() === '退出登录')
    await logout!.trigger('click')
    await flushPromises()

    expect(auth.logout).toHaveBeenCalledOnce()
    expect(toast.toasts.map((item) => item.message)).toEqual(['已退出登录'])
    expect(routerPush).toHaveBeenCalledWith({ name: 'sign-in' })
  })
})
