import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { computed } from 'vue'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { AdminUser, AdminUserPage } from '@/types/console'
import UsersView from '@/views/console/UsersView.vue'

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: true, isRoot: true, user: { id: 1 } }),
}))
vi.mock('@/composables/useFeatureAccess', () => ({
  useFeatureAccess: () => ({ readOnly: computed(() => false) }),
}))

const premiumUser: AdminUser = {
  id: 2,
  username: 'premium-user',
  display_name: '',
  email: '',
  role: 1,
  status: 1,
  group: 'premium',
  available_quota: '0',
  reserved_quota: '0',
  used_quota: '0',
  quota_version: 0,
  request_count: 0,
  invited_count: 0,
  affiliate_quota: '0',
  inviter_id: 0,
  created_time: 1_722_000_000,
  last_login_time: 0,
}

const page: AdminUserPage = {
  items: [premiumUser],
  total: 1,
  page: 1,
  page_size: 20,
  role_counts: { '1': 1 },
  status_counts: { enabled: 1, disabled: 0 },
  group_counts: { default: 3, premium: 1 },
}

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  localStorage.clear()
  vi.restoreAllMocks()
})

async function renderUsers(response: unknown = page) {
  const get = vi
    .spyOn(api, 'get')
    .mockImplementation(async () => structuredClone(response) as never)
  wrapper = mount(UsersView, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  await flushPromises()
  return get
}

function lastListParams(get: ReturnType<typeof vi.spyOn>) {
  const call = get.mock.calls.at(-1)!
  expect(call[0]).toBe('/api/next/admin/users')
  return call[1] as Record<string, unknown>
}

async function chooseGroup(label: RegExp) {
  await wrapper!.get('button[aria-label="User Group"]').trigger('click')
  const option = wrapper!
    .findAll('[role="option"]')
    .find((item) => label.test(item.text()))
  expect(option).toBeDefined()
  await option!.trigger('click')
  await flushPromises()
}

describe('UsersView group filter', () => {
  it('searches only the selected user group and lists all users again after the filter is cleared', async () => {
    const get = await renderUsers()
    expect(lastListParams(get).group).toBeUndefined()

    await wrapper!.get('button[aria-label="User Group"]').trigger('click')
    expect(
      wrapper!.findAll('[role="option"]').map((item) => item.text())
    ).toEqual(['All groups', 'default (3)', 'premium (1)'])
    await wrapper!.get('button[aria-label="User Group"]').trigger('click')

    await chooseGroup(/^premium/)
    expect(lastListParams(get)).toMatchObject({ p: 1, group: 'premium' })
    expect(wrapper!.get('button[aria-label="User Group"]').text()).toContain(
      'premium'
    )

    await chooseGroup(/^All groups$/)
    expect(lastListParams(get).group).toBeUndefined()
  })

  it('offers only all groups when the response has no group counts', async () => {
    // A backend from before the group filter omits group_counts.
    const { group_counts: _omitted, ...legacyPage } = page
    await renderUsers(legacyPage)
    expect(wrapper!.text()).toContain('premium-user')

    await wrapper!.get('button[aria-label="User Group"]').trigger('click')
    expect(
      wrapper!.findAll('[role="option"]').map((item) => item.text())
    ).toEqual(['All groups'])
  })

  it('shows the account group of each user in the table', async () => {
    await renderUsers()
    const headers = wrapper!
      .findAll('th[scope="col"]')
      .map((cell) => cell.text().trim())
    expect(headers).toContain('User Group')
    const premiumRow = wrapper!
      .findAll('tbody tr')
      .find((row) => row.text().includes('premium-user'))
    expect(premiumRow?.text()).toContain('premium')
  })
})
