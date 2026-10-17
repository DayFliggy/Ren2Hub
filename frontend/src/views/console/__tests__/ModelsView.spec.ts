import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  DOMWrapper,
  flushPromises,
  mount,
  type VueWrapper,
} from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import { api } from '@/api/client'
import { publicCatalogApi } from '@/api/publicCatalog'
import { nativeMarketFixture } from '@/utils/__tests__/modelMarketFixtures'
import ModelsView from '@/views/console/ModelsView.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'

let wrapper: VueWrapper | undefined
function mountPage(locale = 'en') {
  wrapper = mount(ModelsView, {
    attachTo: document.body,
    global: {
      plugins: [
        createPinia(),
        createI18n({
          legacy: false,
          locale,
          messages: {
            en: { common: { breadcrumb: 'Breadcrumb' } },
            'zh-CN': { common: { breadcrumb: '面包屑' } },
          },
        }),
      ],
    },
  })
  return wrapper
}
beforeEach(() => {
  localStorage.clear()
  const { pricing, groups } = nativeMarketFixture()
  vi.spyOn(publicCatalogApi, 'pricing').mockResolvedValue(pricing)
  vi.spyOn(api, 'get').mockImplementation(async (path, query) => {
    if (path === '/api/user/self/groups')
      return Object.fromEntries(
        groups.map((group) => [
          group.name,
          { desc: group.description, ratio: group.ratio },
        ])
      )
    if (path === '/api/user/models')
      return groups.find((group) => group.name === query?.group)!.models
    throw new Error(`Unexpected request: ${path}`)
  })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('restored native model market', () => {
  it('restores all three views with honest channel and per-group availability', async () => {
    const page = mountPage()
    await flushPromises()
    expect(page.findAll('[data-model-card]')).toHaveLength(6)
    expect(page.text()).not.toContain('private-only')
    expect(page.text()).toContain(
      '2 available models have no valid pricing record'
    )
    await page
      .findAll('[role="radio"]')
      .find((button) => button.text() === 'Channel shelves')!
      .trigger('click')
    expect(page.get('[data-channel-shelves-unavailable]').text()).toContain(
      'does not provide channel supply'
    )
    expect(api.get).toHaveBeenCalledTimes(4)
    await page
      .findAll('[role="radio"]')
      .find((button) => button.text() === 'Price comparison')!
      .trigger('click')
    const row = page
      .findAll('tbody tr')
      .find((item) => item.text().includes('chat-two'))!
    expect(row.get('[data-group-price="standard"]').text()).toContain(
      'Unavailable in this group'
    )
    expect(row.get('[data-group-price="auto"]').text()).toContain(
      'no single quote'
    )
    expect(row.get('[data-group-price="auto"]').text()).not.toContain('$')
    expect(localStorage.getItem('ren2hub_models_view')).toBe('comparison')
  })
  it('applies the effective group multiplier once in the matrix, cards and detail', async () => {
    const page = mountPage()
    await flushPromises()
    await page
      .findAll('[role="radio"]')
      .find((button) => button.text() === 'Price comparison')!
      .trigger('click')
    const row = page
      .findAll('tbody tr')
      .find((item) => item.text().includes('chat-one'))!
    expect(row.get('[data-base-price]').text()).toContain('$1.000')
    expect(row.get('[data-group-price="standard"]').text()).toContain('$0.200')
    expect(row.get('[data-group-price="standard"]').text()).toContain('$0.600')
    expect(row.get('[data-group-price="premium"]').text()).toContain('$2.000')
    const groupSelect = page
      .findAllComponents(FilterSelect)
      .find((select) => select.props('label') === 'Available group')!
    groupSelect.vm.$emit('update:modelValue', 'standard')
    await page
      .findAll('[role="radio"]')
      .find((button) => button.text() === 'Models')!
      .trigger('click')
    const card = page.get('[data-model-name="chat-one"]')
    expect(card.text()).toContain('$0.200')
    await card
      .findAll('button')
      .find((button) => button.text().includes('View details'))!
      .trigger('click')
    await flushPromises()
    expect(
      document.querySelector('[data-model-market-details]')!.textContent
    ).toContain('$0.200')
    expect(
      document.querySelector('[data-model-market-details]')!.textContent
    ).toContain('POST /v1/chat/completions')
    await new DOMWrapper(
      document.querySelector('[data-model-market-details] select')!
    ).setValue('auto')
    expect(
      document.querySelector('[data-model-market-details]')!.textContent
    ).not.toContain('$')
  })
  it('filters and sorts real models and preserves task expressions without sample prices', async () => {
    const page = mountPage()
    await flushPromises()
    await page
      .findAll('[aria-pressed]')
      .find((button) => button.text() === 'Video')!
      .trigger('click')
    expect(page.findAll('[data-model-card]')).toHaveLength(1)
    const movie = page.get('[data-model-name="movie"]')
    expect(movie.text()).toContain('u("seconds") * 0.2')
    expect(movie.text()).toContain('Provider Video')
    expect(movie.text()).not.toContain('$')
    await page
      .findAll('[aria-pressed]')
      .find((button) => button.text() === 'All endpoints')!
      .trigger('click')
    await page.get('input[type="search"]').setValue('chat')
    const sorting = page
      .findAllComponents(FilterSelect)
      .find((select) => select.props('label') === 'Model ordering')!
    sorting.vm.$emit('update:modelValue', 'input-desc')
    await flushPromises()
    expect(
      page
        .findAll('[data-model-card]')
        .map((card) => card.attributes('data-model-name'))
    ).toEqual(['chat-two', 'chat-one'])
  })
  it('clears stale scoped cards and detail after a permission refresh fails', async () => {
    const page = mountPage()
    await flushPromises()
    await page
      .get('[data-model-name="chat-one"]')
      .findAll('button')
      .find((button) => button.text().includes('View details'))!
      .trigger('click')
    expect(document.querySelector('[data-model-market-details]')).not.toBeNull()
    vi.mocked(api.get).mockRejectedValue(new Error('permission revoked'))
    await page
      .findAll('button')
      .find((button) => button.text() === 'Refresh')!
      .trigger('click')
    await flushPromises()
    expect(page.find('[data-model-card]').exists()).toBe(false)
    expect(document.querySelector('[data-model-market-details]')).toBeNull()
    expect(page.get('[role="alert"]').text()).toContain('permission revoked')
  })
  it('renders the restored Chinese labels without unresolved message keys', async () => {
    const page = mountPage('zh-CN')
    await flushPromises()
    expect(page.text()).toContain('模型聚合')
    expect(page.text()).toContain('渠道货架')
    expect(page.text()).toContain('比价矩阵')
    expect(page.text()).toContain('平台基准价')
    expect(page.text()).not.toContain('billingModes.')
  })
})
