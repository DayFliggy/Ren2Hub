import { flushPromises, shallowMount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: true }),
}))
vi.mock('@/composables/useFeatureAccess', async () => {
  const { ref } = await import('vue')
  return { useFeatureAccess: () => ({ readOnly: ref(false) }) }
})

import FilterSelect from '@/components/common/FilterSelect.vue'
import RedemptionView from '@/views/console/RedemptionView.vue'

it('offers every native status even when the current result page is empty', async () => {
  mocks.get.mockResolvedValue({ items: [], total: 0 })
  const wrapper = shallowMount(RedemptionView, {
    global: {
      renderStubDefaultSlot: true,
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  try {
    await flushPromises()
    const statusFilter = wrapper
      .findAllComponents(FilterSelect)
      .find((filter) => filter.props('label') === 'redemption.allStatuses')!
    expect(statusFilter.props('options').map((option) => option.value)).toEqual(
      ['', 'unused', 'used', 'expired', 'disabled']
    )
    expect(statusFilter.props('options').map((option) => option.label)).toEqual(
      [
        'redemption.allStatuses',
        'redemption.statusUnused',
        'redemption.statusUsed',
        'redemption.statusExpired',
        'redemption.statusDisabled',
      ]
    )
    statusFilter.vm.$emit('update:modelValue', 'expired')
    await flushPromises()
    expect(mocks.get).toHaveBeenLastCalledWith(
      '/api/redemption/search',
      expect.objectContaining({ status: 'expired' }),
      { signal: expect.any(AbortSignal) }
    )
  } finally {
    wrapper.unmount()
  }
})
