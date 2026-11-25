import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import ModelsView from '../ModelsView.vue'
import { modelCatalogFixture } from '@/api/__tests__/modelCatalogFixture'
vi.mock('@/api/client', () => ({ api: { get: vi.fn() } }))
let wrapper: VueWrapper | undefined
function render() {
  wrapper = mount(ModelsView, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return wrapper
}
/** pkg/perf_metrics SummaryAllResult for the fixture model. */
function performanceSummary(models: unknown[]) {
  return {
    summary: models.length
      ? { avg_latency_ms: 850, success_rate: 99.5, avg_tps: 42.25 }
      : null,
    window_start: 1_785_038_400,
    window_end: 1_785_124_800,
    models,
  }
}
const fixturePerformance = {
  model_name: 'fixture-model',
  avg_latency_ms: 850,
  success_rate: 99.5,
  avg_tps: 42.25,
}
function mockFeeds(performance: () => Promise<unknown>) {
  vi.mocked(api.get).mockImplementation(async (url) => {
    if (url === '/api/user/model-catalog') return modelCatalogFixture()
    if (url === '/api/perf-metrics/summary') return performance()
    if (url === '/api/perf-metrics')
      return {
        model_name: 'fixture-model',
        series_schema: 'v1',
        summary: { avg_latency_ms: 850, success_rate: 99.5, avg_tps: 42.25 },
        series: [
          {
            ts: 1_785_117_600,
            avg_ttft_ms: 210,
            avg_latency_ms: 820,
            success_rate: 99.8,
            avg_tps: 44.1,
          },
          {
            ts: 1_785_121_200,
            avg_ttft_ms: 240,
            avg_latency_ms: 880,
            success_rate: 99.2,
            avg_tps: 40.4,
          },
        ],
        window_start: 1_785_038_400,
        window_end: 1_785_124_800,
      }
    throw new Error(`unexpected ${url}`)
  })
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
beforeEach(() =>
  mockFeeds(async () => performanceSummary([fixturePerformance]))
)
afterEach(() => {
  wrapper?.unmount()
  vi.resetAllMocks()
  localStorage.clear()
})
describe('native model catalog page', () => {
  it('loads one catalog and preserves search through all historical visual modes', async () => {
    const view = render()
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith('/api/user/model-catalog', undefined, {
      signal: expect.any(AbortSignal),
    })
    expect(view.findAll('[data-model-card]')).toHaveLength(1)
    await view.get('input[name="model-search"]').setValue('fixture-model')
    for (const label of ['渠道货架', '比价矩阵', '模型聚合']) {
      await view.get(`[role="radio"][aria-label="${label}"]`).trigger('click')
      expect(
        (view.get('input[name="model-search"]').element as HTMLInputElement)
          .value
      ).toBe('fixture-model')
      expect(view.text()).toContain('1 个模型')
    }
    expect(
      vi
        .mocked(api.get)
        .mock.calls.map(([url]) => url)
        .sort()
    ).toEqual(['/api/perf-metrics/summary', '/api/user/model-catalog'])
  })
  it('opens native channel and baseline prices with their supplied endpoint paths', async () => {
    const view = render()
    await flushPromises()
    await view
      .get('[data-model-name="fixture-model"] button[aria-label*="模型详情"]')
      .trigger('click')
    await flushPromises()
    const popup = document.body.querySelector<HTMLElement>('[role="dialog"]')!
    expect(popup.textContent).toContain('/v1/responses')
    expect(popup.textContent).toContain('$0.250')
    expect(popup.textContent).toContain('$1.250')
    expect(
      popup.querySelector('[data-market-baseline] [data-market-telemetry]')
    ).toBeNull()
    expect(popup.querySelector('.market-detail-variant')).toBeNull()
    // The catalog has no capability source, so no placeholder fields are shown.
    expect(popup.textContent).not.toMatch(/上下文|最大输出|工具调用|推理强度/)
    // Channel offer renders its channel-health status block instead of static unavailable telemetry.
    expect(popup.querySelector('[data-catalog-offer-health]')).not.toBeNull()
    // Model-wide 24h hourly series renders from /api/perf-metrics.
    const series = popup.querySelector('[data-model-statistics-series]')
    expect(series).not.toBeNull()
    expect(series?.querySelectorAll('button')).toHaveLength(2)
    // Integrate tab shows base URL and protocol-appropriate cURL.
    const integrateTab = Array.from(
      popup.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    ).find((tab) => tab.textContent?.includes('接入'))!
    integrateTab.click()
    await flushPromises()
    expect(popup.textContent).toContain('https://api.example.test')
    expect(popup.textContent).toContain('curl ')
  })
  it('does not render permission-scoped prices after a catalog failure', async () => {
    vi.mocked(api.get).mockRejectedValue(new Error('denied'))
    const view = render()
    await flushPromises()
    expect(view.find('[data-model-card]').exists()).toBe(false)
    expect(view.find('[data-offer-id]').exists()).toBe(false)
  })
})
describe('native model performance', () => {
  it('shows the model-wide success rate, duration and TPS from perf-metrics', async () => {
    const view = render()
    await flushPromises()
    expect(api.get).toHaveBeenCalledWith(
      '/api/perf-metrics/summary',
      { hours: 24 },
      { signal: expect.any(AbortSignal) }
    )
    const stats = view.get('[data-model-statistics="ready"]')
    expect(stats.attributes('aria-label')).toContain('全部渠道 · 近 24 小时')
    expect(stats.text()).not.toContain('全部渠道 · 近 24 小时')
    expect(stats.text()).toContain('99.5%')
    expect(stats.text()).toContain('850 ms')
    expect(stats.text()).toContain('42.25')
    expect(view.find('[data-model-performance-error]').exists()).toBe(false)
    await view.get('[role="radio"][aria-label="渠道货架"]').trigger('click')
    expect(view.find('[data-model-statistics]').exists()).toBe(false)
    expect(
      view.get('[data-market-shelves]').find('[data-market-telemetry]').exists()
    ).toBe(false)
    expect(
      view
        .get('[data-market-shelves]')
        .find('[data-catalog-channel-health]')
        .exists()
    ).toBe(true)
    expect(
      view
        .get('[data-market-shelves]')
        .find('[data-catalog-offer-health]')
        .exists()
    ).toBe(true)
  })
  it('reports models without samples instead of a zero success rate', async () => {
    mockFeeds(async () => performanceSummary([]))
    const view = render()
    await flushPromises()
    expect(view.find('[data-model-statistics]').exists()).toBe(false)
    expect(view.find('[data-model-statistics-unavailable]').exists()).toBe(
      false
    )
    expect(view.get('[data-model-card]').text()).not.toMatch(/\d%|TPS/)
  })
  it('shows a load error without inventing statistics and retries on demand', async () => {
    mockFeeds(async () => {
      throw new ApiError('perf-metrics unavailable', { status: 500 })
    })
    const view = render()
    await flushPromises()
    expect(view.find('[data-model-statistics]').exists()).toBe(false)
    expect(view.get('[data-model-performance-error]').text()).toContain(
      '统计加载失败'
    )
    expect(view.get('[data-model-card]').text()).not.toMatch(/\d%|TPS/)
    expect(view.get('[data-channel-quote]').text()).toContain('$0.250')
    const alert = view.get('[data-model-performance-error]')
    expect(alert.attributes('role')).toBe('alert')
    expect(alert.text()).toContain('perf-metrics unavailable')

    mockFeeds(async () => performanceSummary([fixturePerformance]))
    await alert.get('button').trigger('click')
    await flushPromises()
    expect(view.get('[data-model-statistics="ready"]').text()).toContain(
      '99.5%'
    )
    expect(view.find('[data-model-performance-error]').exists()).toBe(false)
  })
})
