import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
const toast = vi.hoisted(() => ({
  success: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/api/console', () => ({ api: mocks }))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
vi.mock('@/utils/spreadsheetExport', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('@/utils/spreadsheetExport')>()
  return { ...actual, serializeSpreadsheet: vi.fn(actual.serializeSpreadsheet) }
})
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: true }),
}))
vi.mock('@/composables/useFeatureAccess', async () => {
  const { ref } = await import('vue')
  return { useFeatureAccess: () => ({ readOnly: ref(false) }) }
})

import FilterSelect from '@/components/common/FilterSelect.vue'
import RedemptionView from '@/views/console/RedemptionView.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { REDEMPTION_EXPORT_MAX_ROWS } from '@/composables/useAdminRedemption'
import { serializeSpreadsheet } from '@/utils/spreadsheetExport'

function mountView() {
  return shallowMount(RedemptionView, {
    global: {
      renderStubDefaultSlot: true,
      // The page actions, including export, render in the breadcrumb slot.
      stubs: {
        PageBreadcrumb: { template: '<div><slot name="action" /></div>' },
      },
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
}

it('offers every native status even when the current result page is empty', async () => {
  mocks.get.mockResolvedValue({ items: [], total: 0 })
  const wrapper = mountView()
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

// Native rows: numeric quota and the redeemer as used_user_id.
function nativeRedemptions(count: number, firstId: number) {
  return Array.from({ length: count }, (_, index) => ({
    id: firstId + index,
    name: 'code',
    key: `code-${firstId + index}`,
    status: 3,
    quota: 500000,
    used_user_id: 40 + index,
    created_time: 1,
    redeemed_time: 2,
    expired_time: 0,
  }))
}

// Export pages are requested with page_size 100; pageSizes[p - 1] rows each.
function serveExportPages(pageSizes: number[]) {
  mocks.get.mockImplementation(
    async (_url: string, params: Record<string, unknown>) => {
      if (params.page_size !== 100)
        return { items: nativeRedemptions(1, 1), total: 1 }
      const p = Number(params.p)
      return {
        items: nativeRedemptions(pageSizes[p - 1] ?? 0, (p - 1) * 100 + 1),
        total: 10_000,
      }
    }
  )
}

function exportButton(wrapper: ReturnType<typeof mountView>) {
  return wrapper
    .findAllComponents(ConsoleButton)
    .find((button) => button.text().includes('redemption.exportCsv'))!
}

function statusSelect(wrapper: ReturnType<typeof mountView>) {
  return wrapper
    .findAllComponents(FilterSelect)
    .find((filter) => filter.props('label') === 'redemption.allStatuses')!
}

beforeEach(() => {
  vi.clearAllMocks()
  Object.assign(URL, {
    createObjectURL: vi.fn(() => 'blob:redemption-export'),
    revokeObjectURL: vi.fn(),
  })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
})

it('exports every page for the filters in effect when the export starts', async () => {
  serveExportPages([100, 2])
  const wrapper = mountView()
  try {
    await flushPromises()
    statusSelect(wrapper).vm.$emit('update:modelValue', 'used')
    await flushPromises()

    exportButton(wrapper).vm.$emit('click')
    statusSelect(wrapper).vm.$emit('update:modelValue', 'disabled')
    await flushPromises()

    const exportCalls = mocks.get.mock.calls.filter(
      ([, params]) => params.page_size === 100
    )
    expect(
      exportCalls.map(([url, params]) => [url, params.p, params.status])
    ).toEqual([
      ['/api/redemption/search', 1, 3],
      ['/api/redemption/search', 2, 3],
    ])
    const [headers, rows] = vi.mocked(serializeSpreadsheet).mock.calls[0]!
    expect(headers).toContain('RedeemerID')
    expect(headers.join(',').toLowerCase()).not.toContain('email')
    expect(rows).toHaveLength(102)
    expect(rows[101]?.[0]).toBe(102)
    expect(rows[101]?.[headers.indexOf('RedeemerID')]).toBe(41)
    expect(toast.success).toHaveBeenCalledWith('redemption.exported')
    expect(toast.warning).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})

it('warns when the export stops at the row limit', async () => {
  serveExportPages(Array(101).fill(100))
  const wrapper = mountView()
  try {
    await flushPromises()
    exportButton(wrapper).vm.$emit('click')
    await flushPromises()

    const [, rows] = vi.mocked(serializeSpreadsheet).mock.calls[0]!
    expect(rows).toHaveLength(REDEMPTION_EXPORT_MAX_ROWS)
    expect(toast.warning).toHaveBeenCalledWith('redemption.exportTruncated')
    expect(toast.success).not.toHaveBeenCalled()
  } finally {
    wrapper.unmount()
  }
})
