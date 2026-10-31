import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/types'
import {
  parseUsageDiscountEstimate,
  parseUsageDiscountSettings,
  parseUsageDiscountStatus,
} from '@/api/usageDiscount'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { UsageDiscountSettings } from '@/types/usageDiscount'
import UsageDiscountView from '../UsageDiscountView.vue'
// Generated from the real handlers by controller/frontend_contract_test.go.
import configResponse from '../../../../tests/fixtures/native/usage-discount.get.json'
import statusResponse from '../../../../tests/fixtures/native/usage-discount-status.get.json'
import estimateResponse from '../../../../tests/fixtures/native/usage-discount-estimate.post.json'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  status: vi.fn(),
  estimate: vi.fn(),
}))
// Only the requests are replaced; the response parsers stay real.
vi.mock('@/api/usageDiscount', async (original) => ({
  ...(await original<typeof import('@/api/usageDiscount')>()),
  usageDiscountApi: {
    load: mocks.load,
    save: mocks.save,
    status: mocks.status,
    estimate: mocks.estimate,
  },
}))

const loaded = parseUsageDiscountSettings(configResponse.data)
/** The same document saved elsewhere as `version` with another fixed rate. */
function serverVersion(version: number, fixed: string): UsageDiscountSettings {
  return {
    ...loaded,
    version,
    created_at: 1793300000,
    created_by: 2,
    config: { ...loaded.config, fixed_rate: fixed },
  }
}

let wrapper: VueWrapper | undefined
async function setup() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/usage-discount', component: UsageDiscountView }],
  })
  await router.push('/usage-discount')
  wrapper = mount(
    { template: '<RouterView />' },
    {
      global: {
        plugins: [
          router,
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
        stubs: { teleport: true },
      },
    }
  )
  await flushPromises()
  return wrapper
}
function button(
  scope: VueWrapper | ReturnType<VueWrapper['get']>,
  text: string
) {
  const found = scope.findAll('button').find((item) => item.text() === text)
  if (!found) throw new Error(`Missing button: ${text}`)
  return found
}
function field(scope: VueWrapper, caption: string) {
  const label = scope
    .findAll('label[for]')
    .find((item) => item.text() === caption)
  if (!label) throw new Error(`Missing field: ${caption}`)
  return scope.get<HTMLInputElement>(`#${label.attributes('for')}`)
}

beforeEach(() => {
  vi.clearAllMocks()
  wrapper?.unmount()
  wrapper = undefined
  mocks.load.mockResolvedValue(loaded)
  mocks.status.mockResolvedValue(parseUsageDiscountStatus(statusResponse.data))
  mocks.estimate.mockResolvedValue(
    parseUsageDiscountEstimate(estimateResponse.data)
  )
})

describe('usage discount editor', () => {
  it('keeps the draft through a version conflict and adopts the server version only on reload', async () => {
    const page = await setup()
    await field(page, 'Fixed discount').setValue('1')
    mocks.save.mockRejectedValueOnce(
      new ApiError('usage_discount_version_conflict', {
        status: 409,
        code: 'usage_discount_version_conflict',
      })
    )
    mocks.load.mockResolvedValue(serverVersion(3, '0.02'))

    await button(page, 'Save configuration').trigger('click')
    await flushPromises()

    expect(mocks.save).toHaveBeenCalledWith(
      0,
      expect.objectContaining({ fixed_rate: '0.01' })
    )
    expect(page.get('[data-discount-conflict]').text()).toContain(
      'now version 3'
    )
    expect(field(page, 'Fixed discount').element.value).toBe('1')
    expect(page.text()).toContain('Unsaved changes')
    // Saving again would only repeat the conflict against version 0.
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(true)

    await button(page, 'Load server version').trigger('click')
    const dialog = page.getComponent(ConfirmDialog)
    expect(dialog.props('open')).toBe(true)
    dialog.vm.$emit('confirm')
    await flushPromises()

    expect(page.find('[data-discount-conflict]').exists()).toBe(false)
    expect(field(page, 'Fixed discount').element.value).toBe('2')
    expect(page.text()).not.toContain('Unsaved changes')
    expect(page.text()).toContain('Version 3')
  })

  it('reports a configuration the server rejects and keeps the draft for another try', async () => {
    const page = await setup()
    await field(page, 'Fixed discount').setValue('1')
    mocks.save.mockRejectedValueOnce(
      new ApiError('usage_discount_invalid_config: timezone', {
        status: 400,
        code: 'usage_discount_invalid_config',
      })
    )
    await button(page, 'Save configuration').trigger('click')
    await flushPromises()

    const alert = page.get('[role="alert"]').text()
    expect(alert).toContain('Saving failed; your draft is kept.')
    expect(alert).toContain('The server rejected this configuration.')
    expect(alert).toContain('usage_discount_invalid_config: timezone')
    expect(page.find('[data-discount-conflict]').exists()).toBe(false)
    expect(field(page, 'Fixed discount').element.value).toBe('1')
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(false)
  })

  it('saves a tier entered in USD and percent with the loaded version', async () => {
    const page = await setup()
    const site = page.get('[data-discount-table="site"]')
    await button(site, 'Add tier').trigger('click')
    await site.get('input[aria-label="Tier 1 threshold (USD)"]').setValue('25')
    await site.get('input[aria-label="Tier 1 discount (%)"]').setValue('1.5')
    await site.findAll('[role="radio"]')[2]!.trigger('click')
    mocks.save.mockImplementation(async (_version, config) => ({
      ...loaded,
      version: 1,
      created_at: 1793300000,
      created_by: 1,
      config,
    }))

    await button(page, 'Save configuration').trigger('click')
    await flushPromises()

    expect(mocks.save).toHaveBeenCalledWith(
      0,
      expect.objectContaining({
        site: {
          mode: 'on',
          tiers: [{ threshold_quota: '12500000', rate: '0.015' }],
        },
      })
    )
    expect(page.text()).toContain('Saved as version 1')
    expect(page.text()).not.toContain('Unsaved changes')
    // The saved version changes what the status reports.
    expect(mocks.status).toHaveBeenCalledTimes(2)
  })

  it('blocks saving and estimating an invalid draft and says why inline', async () => {
    const page = await setup()
    await field(page, 'Fixed discount').setValue('6')

    expect(page.text()).toContain('Cannot exceed the 5% cap.')
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(true)
    expect(
      button(page, 'Estimate this draft').element.matches(':disabled')
    ).toBe(true)
    expect(page.text()).toContain(
      'Fix the errors in the draft before saving or estimating.'
    )
  })

  it('marks an estimate stale as soon as the draft changes', async () => {
    const page = await setup()
    await button(page, 'Estimate this draft').trigger('click')
    await flushPromises()

    expect(mocks.estimate).toHaveBeenCalledWith(
      loaded.config,
      expect.anything()
    )
    const estimate = page.get('[data-discount-estimate]')
    expect(estimate.text()).toContain('an approximation, not a bill')
    expect(estimate.text()).toContain('2026-09-30 to 2026-10-29 (30 days)')
    expect(estimate.text()).toContain('Under about 60 days')
    expect(page.find('[data-estimate-stale]').exists()).toBe(false)

    await field(page, 'Discount cap').setValue('6')
    expect(page.find('[data-estimate-stale]').exists()).toBe(true)
    expect(page.get('[data-estimate-result]').classes()).toContain('is-stale')
  })

  it('keeps the editor usable when the status cannot be read', async () => {
    mocks.status.mockRejectedValue(
      new ApiError('usage discount status: query timeout', {
        status: 503,
        code: 'usage_discount_unavailable',
      })
    )
    const page = await setup()

    const status = page.get('[data-discount-status]').text()
    expect(status).toContain('Could not read the status.')
    expect(status).toContain('Usage data is temporarily unreadable.')
    await field(page, 'Fixed discount').setValue('1')
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(false)
  })
})
