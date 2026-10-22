import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeAll, beforeEach, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api: mocks }))

import FilterSelect from '@/components/common/FilterSelect.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import UsersView from '@/views/console/UsersView.vue'

/** One upstream `GET /api/user/` item; the list query is unscoped. */
function rawUser(id: number, username: string, DeletedAt: string | null) {
  return {
    id,
    username,
    display_name: '',
    email: '',
    role: 1,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    max_concurrency: 0,
    aff_count: 0,
    aff_quota: 0,
    aff_history_quota: 0,
    aff_frozen_quota: 0,
    inviter_id: 0,
    created_at: 1,
    last_login_at: 0,
    DeletedAt,
  }
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
  useAuthStore().persist({
    id: 1,
    username: 'root',
    display_name: '',
    email: '',
    role: 100,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: {} },
  })
  mocks.get.mockReset()
  mocks.get.mockResolvedValue({
    items: [
      rawUser(2, 'active.member', null),
      rawUser(3, 'removed.member', '2026-10-20T08:00:00+08:00'),
    ],
    total: 2,
  })
})

function render() {
  return mount(UsersView, {
    global: {
      plugins: [i18n],
      stubs: {
        UserFormModal: true,
        UserQuotaModal: true,
        UserAffiliateModal: true,
        SecurityProofDialog: true,
        ConfirmDialog: true,
      },
    },
  })
}

function rowFor(wrapper: ReturnType<typeof render>, selector: string) {
  return wrapper
    .findAll(selector)
    .find((row) => row.text().includes('removed.member'))!
}

it('shows a soft-deleted user as deleted with no selection or actions', async () => {
  const wrapper = render()
  try {
    await flushPromises()
    for (const row of [
      rowFor(wrapper, 'tbody tr'),
      rowFor(wrapper, '[data-user-mobile-row]'),
    ]) {
      expect(row.text()).toContain('Deleted')
      expect(row.find('input[type="checkbox"]').exists()).toBe(false)
      expect(row.findAll('button')).toHaveLength(0)
    }
    const active = wrapper
      .findAll('[data-user-mobile-row]')
      .find((row) => row.text().includes('active.member'))!
    expect(active.find('input[type="checkbox"]').exists()).toBe(true)
  } finally {
    wrapper.unmount()
  }
})

it('filters deleted users with the native status -1', async () => {
  const wrapper = render()
  try {
    await flushPromises()
    const filter = wrapper
      .findAllComponents(FilterSelect)
      .find((select) => select.props('label') === 'Status')!
    expect(filter.props('options').map((option) => option.value)).toEqual([
      '',
      '1',
      '2',
      '-1',
    ])
    filter.vm.$emit('update:modelValue', '-1')
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/user/search',
      expect.objectContaining({ status: '-1' }),
      { signal: expect.any(AbortSignal) }
    )
  } finally {
    wrapper.unmount()
  }
})
