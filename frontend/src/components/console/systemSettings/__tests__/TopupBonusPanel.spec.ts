import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/types'
import {
  parseTopupBonusEstimate,
  parseTopupBonusSettings,
} from '@/api/topupBonus'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import type { TopupBonusSettings } from '@/types/topupBonus'
import TopupBonusPanel from '../TopupBonusPanel.vue'
// Generated from the real handlers by controller/frontend_contract_test.go.
import configResponse from '../../../../../tests/fixtures/native/topup-bonus.get.json'
import estimateResponse from '../../../../../tests/fixtures/native/topup-bonus-estimate.post.json'

const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  save: vi.fn(),
  status: vi.fn(),
  estimate: vi.fn(),
}))
// Only the requests are replaced; the response parsers stay real.
vi.mock('@/api/topupBonus', async (original) => ({
  ...(await original<typeof import('@/api/topupBonus')>()),
  topupBonusApi: {
    load: mocks.load,
    save: mocks.save,
    status: mocks.status,
    estimate: mocks.estimate,
  },
}))

const loaded = parseTopupBonusSettings(configResponse.data)
/** The same document saved elsewhere as `version` with another fixed rate. */
function serverVersion(version: number, fixed: string): TopupBonusSettings {
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
    routes: [
      {
        path: '/system-settings/billing/topup-bonus',
        component: TopupBonusPanel,
      },
    ],
  })
  await router.push('/system-settings/billing/topup-bonus')
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
  mocks.estimate.mockResolvedValue(
    parseTopupBonusEstimate(estimateResponse.data)
  )
})

describe('top-up bonus editor', () => {
  it('keeps the draft through a version conflict and adopts the server version only on reload', async () => {
    const page = await setup()
    await field(page, 'Fixed bonus').setValue('1')
    mocks.save.mockRejectedValueOnce(
      new ApiError('topup_bonus_version_conflict', {
        status: 409,
        code: 'topup_bonus_version_conflict',
      })
    )
    mocks.load.mockResolvedValue(serverVersion(3, '0.02'))

    await button(page, 'Save configuration').trigger('click')
    await flushPromises()

    expect(mocks.save).toHaveBeenCalledWith(
      0,
      expect.objectContaining({ fixed_rate: '0.01' })
    )
    expect(page.get('[data-bonus-conflict]').text()).toContain('now version 3')
    expect(field(page, 'Fixed bonus').element.value).toBe('1')
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

    expect(page.find('[data-bonus-conflict]').exists()).toBe(false)
    expect(field(page, 'Fixed bonus').element.value).toBe('2')
    expect(page.text()).not.toContain('Unsaved changes')
    expect(page.text()).toContain('Version 3')
  })

  it('reports a configuration the server rejects and keeps the draft for another try', async () => {
    const page = await setup()
    await field(page, 'Fixed bonus').setValue('1')
    mocks.save.mockRejectedValueOnce(
      new ApiError('topup_bonus_invalid_config: timezone', {
        status: 400,
        code: 'topup_bonus_invalid_config',
      })
    )
    await button(page, 'Save configuration').trigger('click')
    await flushPromises()

    const alert = page.get('[role="alert"]').text()
    expect(alert).toContain('Saving failed; your draft is kept.')
    expect(alert).toContain('The server rejected this configuration.')
    expect(alert).toContain('topup_bonus_invalid_config: timezone')
    expect(page.find('[data-bonus-conflict]').exists()).toBe(false)
    expect(field(page, 'Fixed bonus').element.value).toBe('1')
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(false)
  })

  it('saves a credit amount tier as its own table next to the usage tiers', async () => {
    const page = await setup()
    const amount = page.get('[data-bonus-table="amount"]')
    expect(amount.text()).toContain('Credit amount tiers')
    expect(amount.text()).toContain(
      'for every online top-up, redemption code and administrator addition'
    )
    await button(amount, 'Add tier').trigger('click')
    await amount
      .get('input[aria-label="Tier 1 threshold (USD)"]')
      .setValue('100')
    await amount.get('input[aria-label="Tier 1 bonus (%)"]').setValue('2')
    await amount.findAll('[role="radio"]')[2]!.trigger('click')
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
        amount: {
          mode: 'on',
          tiers: [{ threshold_quota: '50000000', rate: '0.02' }],
        },
      })
    )
  })

  it('saves a tier entered in USD and percent with the loaded version', async () => {
    const page = await setup()
    const site = page.get('[data-bonus-table="site"]')
    await button(site, 'Add tier').trigger('click')
    await site.get('input[aria-label="Tier 1 threshold (USD)"]').setValue('25')
    await site.get('input[aria-label="Tier 1 bonus (%)"]').setValue('1.5')
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
    expect(mocks.status).not.toHaveBeenCalled()
  })

  it('blocks saving and estimating an invalid draft and says why inline', async () => {
    const page = await setup()
    await field(page, 'Fixed bonus').setValue('6')

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
    const estimate = page.get('[data-bonus-estimate]')
    expect(estimate.text()).toContain('an estimate, not a bill')
    expect(estimate.text()).toContain('2026-09-30 to 2026-10-29 (30 days)')
    expect(estimate.text()).toContain('Under about 60 days')
    expect(
      page
        .findAll('[data-estimate-source]')
        .map((item) => item.attributes('data-estimate-source'))
    ).toEqual(['topup', 'redemption', 'admin_quota'])
    expect(page.get('[data-estimate-source="redemption"]').text()).toContain(
      'Redemption codes: 0'
    )
    expect(page.find('[data-estimate-stale]').exists()).toBe(false)

    await field(page, 'Bonus cap').setValue('6')
    expect(page.find('[data-estimate-stale]').exists()).toBe(true)
    expect(page.get('[data-estimate-result]').classes()).toContain('is-stale')
  })

  it('loads only configuration and never requests runtime diagnostics', async () => {
    const page = await setup()
    expect(mocks.load).toHaveBeenCalledOnce()
    expect(mocks.status).not.toHaveBeenCalled()
    expect(page.find('[data-bonus-status]').exists()).toBe(false)
    await field(page, 'Fixed bonus').setValue('1')
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(false)
  })

  it('keeps window drafts and exposes errors when their window is hidden', async () => {
    const page = await setup()
    const day = page.get('[data-bonus-table="day"]')
    const amount = day.get<HTMLInputElement>(
      'input[aria-label="Tier 2 threshold (USD)"]'
    )
    await amount.setValue('1')
    const tabs = page.findAll('[role="tab"]')
    await tabs[2]!.trigger('click')
    expect(day.isVisible()).toBe(false)
    expect(tabs[0]!.text()).toContain('1 to fix')
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(true)
    expect(
      button(page, 'Estimate this draft').element.matches(':disabled')
    ).toBe(true)
    await tabs[2]!.trigger('keydown', { key: 'Home' })
    expect(tabs[0]!.attributes('aria-selected')).toBe('true')
    expect(amount.element.value).toBe('1')
    await amount.setValue('15')
    expect(tabs[0]!.text()).not.toContain('to fix')
    await tabs[0]!.trigger('keydown', { key: 'End' })
    expect(tabs[2]!.attributes('aria-selected')).toBe('true')
    expect(page.get('[data-bonus-table="month"]').isVisible()).toBe(true)
  })

  it('retains mode edits and invalid tiers when switching windows', async () => {
    const page = await setup()
    const tabs = page.findAll('[role="tab"]')
    const day = page.get('[data-bonus-table="day"]')
    await day.findAll('[role="radio"]')[0]!.trigger('click')
    await button(day, 'Add tier').trigger('click')
    await tabs[1]!.trigger('click')
    expect(tabs[0]!.text()).toContain('to fix')
    await tabs[0]!.trigger('click')
    expect(day.findAll('[role="radio"]')[0]!.attributes('aria-checked')).toBe(
      'true'
    )
    expect(day.findAll('[data-bonus-tier]')).toHaveLength(6)
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(true)
    await day.get('[aria-label="Remove tier 6"]').trigger('click')
    expect(
      button(page, 'Save configuration').element.matches(':disabled')
    ).toBe(false)
  })
})
