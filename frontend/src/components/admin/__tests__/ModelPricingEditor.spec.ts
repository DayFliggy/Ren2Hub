import { flushPromises, mount } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import ModelPricingEditor from '../ModelPricingEditor.vue'
import { modelPricingApi, type PriceVersion } from '@/api/modelPricing'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const price: PriceVersion = {
  contract_version: 2,
  calculation_version: 'billing-v2',
  result_unit: 'usd',
  id: 3,
  model: 'test',
  mode: 'unit_price',
  prices: [{ quantity: 'p', unit: 'per_million_tokens', price: '1' }],
  created_at: 1,
  hash: 'hash',
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => vi.restoreAllMocks())
beforeEach(() =>
  vi.spyOn(modelPricingApi, 'taskSchema').mockResolvedValue({
    model: 'test',
    plugin_key: '',
    usage_schema: null,
    manifest_schema: null,
    review_required: false,
    plugins: [],
  })
)
function setup() {
  vi.spyOn(modelPricingApi, 'load').mockResolvedValue([price])
  const publish = vi
    .spyOn(modelPricingApi, 'publish')
    .mockResolvedValue({ ...price, id: 4 })
  const wrapper = mount(ModelPricingEditor, {
    props: { modelName: 'test' },
    global: { plugins: [i18n] },
  })
  return { wrapper, publish }
}
describe('model price editor', () => {
  it('converts per-image prices only after review and preserves the saved source', async () => {
    const { wrapper, publish } = setup()
    const original: PriceVersion = {
      ...price,
      prices: [{ quantity: 'images', unit: 'per_image', price: '0.00123' }],
    }
    vi.mocked(modelPricingApi.load).mockResolvedValue([original])
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    await wrapper
      .findAll('button')
      .find(
        (button) => button.text() === 'Convert unit prices to expression draft'
      )!
      .trigger('click')
    expect(wrapper.get('[data-price-conversion-preview]').text()).toContain(
      'v1:fixed(0.00123) * image_count'
    )
    expect(wrapper.find('textarea[spellcheck=false]').exists()).toBe(false)
    await wrapper.get('[data-apply-price-conversion]').trigger('click')
    expect(
      wrapper.get<HTMLTextAreaElement>('textarea[spellcheck=false]').element
        .value
    ).toBe('v1:fixed(0.00123) * image_count')
    expect(publish).not.toHaveBeenCalled()
    await wrapper.vm.save('test')
    expect(publish).toHaveBeenCalledWith(
      expect.objectContaining({
        mode: 'expression',
        prices: [],
        expression: 'v1:fixed(0.00123) * image_count',
      }),
      3,
      undefined
    )
    expect(original.prices).toEqual([
      { quantity: 'images', unit: 'per_image', price: '0.00123' },
    ])
    wrapper.unmount()
  })
  it('refreshes the publication schema on model rename without resetting the price draft', async () => {
    vi.useFakeTimers()
    const { wrapper } = setup()
    try {
      await flushPromises()
      await wrapper.get('input[inputmode=decimal]').setValue('7.000001')
      await wrapper.setProps({ modelName: 'other-model' })
      await vi.advanceTimersByTimeAsync(300)
      expect(modelPricingApi.taskSchema).toHaveBeenLastCalledWith(
        'other-model',
        undefined,
        expect.any(AbortSignal)
      )
      expect(
        wrapper.get<HTMLInputElement>('input[inputmode=decimal]').element.value
      ).toBe('7.000001')
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })
  it('keeps a saved schema and expression visible until explicit adoption and forwards plugin validation', async () => {
    const savedSchema = {
      version: 1 as const,
      hash: 'old',
      fields: {
        seconds: {
          type: 'number' as const,
          unit: 'second',
          source: 'completion_fact' as const,
          required: true,
        },
        resolution: {
          type: 'string' as const,
          source: 'request_fixed' as const,
          required: true,
          enum: ['720p', '1080p'],
        },
      },
    }
    const current = {
      ...savedSchema,
      hash: 'current',
      fields: {
        ...savedSchema.fields,
        resolution: { ...savedSchema.fields.resolution, enum: ['1080p'] },
      },
    }
    const existing = {
      ...price,
      mode: 'expression' as const,
      expression:
        'v2:u("resolution") == "720p" ? u("seconds") * 0.2 : u("seconds") * 0.4',
      prices: [],
      usage_schema: savedSchema,
    }
    const { wrapper, publish } = setup()
    vi.mocked(modelPricingApi.load).mockResolvedValue([existing])
    vi.mocked(modelPricingApi.taskSchema).mockResolvedValue({
      model: 'test',
      plugin_key: 'video',
      usage_schema: current,
      manifest_schema: {},
      review_required: false,
      plugins: [{ key: 'video', name: 'Video', version: '2' }],
    })
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    const expression = wrapper.get<HTMLTextAreaElement>(
      'textarea[spellcheck=false]'
    )
    expect(expression.element.value).toContain('720p')
    await wrapper.vm.save('test')
    expect(publish).not.toHaveBeenCalled()
    await wrapper.get('[data-adopt-task-schema]').trigger('click')
    expect(expression.element.value).toContain('720p')
    publish.mockRejectedValueOnce(new Error('schema enum is no longer allowed'))
    await expect(wrapper.vm.save('test')).rejects.toThrow('schema enum')
    expect(publish).toHaveBeenLastCalledWith(
      expect.objectContaining({
        usage_schema: current,
        expression: existing.expression,
      }),
      3,
      'video'
    )
    await wrapper.get('[data-condition-field]').setValue('resolution')
    const conditionSelect = wrapper.findAll('select[aria-label=resolution]')[0]
    await conditionSelect.setValue('1080p')
    await wrapper.get('[data-add-schema-condition]').trigger('click')
    expect(expression.element.value).toContain('u("resolution") == "1080p"')
    wrapper.unmount()
  })
  it('keeps exact USD while showing currency and converts only on explicit action', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    const usd = wrapper.get('input[inputmode=decimal]')
    await usd.setValue('1.123456789123456789')
    await wrapper.get('[data-exchange-rate]').setValue('7')
    expect((usd.element as HTMLInputElement).value).toBe('1.123456789123456789')
    await wrapper.get('input[aria-label="Local amount"]').setValue('14')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Convert to USD')!
      .trigger('click')
    expect((usd.element as HTMLInputElement).value).toBe('2')
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('converts a unit draft and applies a frozen-time rule without publishing', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper
      .findAll('button')
      .find(
        (button) => button.text() === 'Convert unit prices to expression draft'
      )!
      .trigger('click')
    expect(wrapper.find('textarea[spellcheck=false]').exists()).toBe(false)
    expect(wrapper.get('[data-price-conversion-preview]').text()).toContain(
      'v1:p * 1'
    )
    expect(publish).not.toHaveBeenCalled()
    await wrapper.get('[data-apply-price-conversion]').trigger('click')
    const expression = wrapper.get('textarea[spellcheck=false]')
    expect((expression.element as HTMLTextAreaElement).value).toBe('v1:p * 1')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Add rule to expression')!
      .trigger('click')
    expect((expression.element as HTMLTextAreaElement).value).toContain(
      'hour("Asia/Shanghai") >= 0 && hour("Asia/Shanghai") < 6'
    )
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('previews through the server without publishing and clears stale results after edits', async () => {
    const { wrapper, publish } = setup()
    const preview = vi.spyOn(modelPricingApi, 'preview').mockResolvedValue({
      quote: {},
      charge: {
        lines: [],
        final_quota: '500',
        total_usd: '0.001',
        unrounded_quota: '500',
        rounding_delta: '0',
        calculation_version: 'billing-v2',
        rounding_version: 'half-up-positive-minimum-v1',
      },
    })
    await flushPromises()
    await wrapper.get('[data-price-preview]').trigger('click')
    await flushPromises()
    expect(preview).toHaveBeenCalledWith(
      expect.objectContaining({
        channel_ratio: '1',
        price: expect.objectContaining({ contract_version: 2 }),
      })
    )
    expect(wrapper.get('[data-price-preview-result]').text()).toContain(
      '500 quota'
    )
    expect(publish).not.toHaveBeenCalled()
    await wrapper.get('input[inputmode=decimal]').setValue('2')
    expect(wrapper.find('[data-price-preview-result]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('publishes exact decimal text and leaves the loaded immutable version intact', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper
      .get('input[inputmode=decimal]')
      .setValue('1.000000000000000001')
    await wrapper.vm.save('test')
    expect(publish).toHaveBeenCalledWith(
      expect.objectContaining({
        prices: [
          {
            quantity: 'p',
            unit: 'per_million_tokens',
            price: '1.000000000000000001',
          },
        ],
      }),
      3,
      undefined
    )
    expect(price.prices[0]?.price).toBe('1')
    wrapper.unmount()
  })
  it.each(['rename', 'reopen'] as const)(
    'discards a pending preview after %s changes its context',
    async (change) => {
      const { wrapper } = setup()
      let resolve!: (
        value: Awaited<ReturnType<typeof modelPricingApi.preview>>
      ) => void
      vi.spyOn(modelPricingApi, 'preview').mockReturnValue(
        new Promise((done) => {
          resolve = done
        })
      )
      await flushPromises()
      await wrapper.get('[data-price-preview]').trigger('click')
      if (change === 'rename') {
        await wrapper.setProps({ modelName: 'renamed-model' })
      } else {
        await wrapper.setProps({ open: false })
        await wrapper.setProps({ open: true })
        await flushPromises()
      }
      resolve({
        quote: {},
        charge: {
          lines: [],
          final_quota: '500',
          total_usd: '0.001',
          unrounded_quota: '500',
          rounding_delta: '0',
          calculation_version: 'billing-v2',
          rounding_version: 'half-up-positive-minimum-v1',
        },
      })
      await flushPromises()
      expect(wrapper.find('[data-price-preview-result]').exists()).toBe(false)
      wrapper.unmount()
    }
  )
  it('rejects concurrent target changes before publishing', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper.get('input[inputmode=decimal]').setValue('2')
    vi.mocked(modelPricingApi.load).mockResolvedValue([{ ...price, id: 5 }])
    await expect(wrapper.vm.validate('test')).rejects.toThrow(
      'changed elsewhere'
    )
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps edits made during publication dirty against the published version', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    const amount = wrapper.get('input[inputmode=decimal]')
    await amount.setValue('2')
    let resolve!: (price: PriceVersion) => void
    publish.mockReturnValueOnce(
      new Promise((done) => {
        resolve = done
      })
    )
    const saving = wrapper.vm.save('test')
    await flushPromises()
    expect(publish).toHaveBeenCalledTimes(1)
    await amount.setValue('3')
    const published = {
      ...price,
      id: 4,
      prices: [{ ...price.prices[0]!, price: '2' }],
    }
    resolve(published)
    await saving
    vi.mocked(modelPricingApi.load).mockResolvedValue([published])
    await wrapper.vm.save('test')
    expect(publish).toHaveBeenCalledTimes(2)
    expect(publish).toHaveBeenLastCalledWith(
      expect.objectContaining({ prices: [{ ...price.prices[0], price: '3' }] }),
      4,
      undefined
    )
    wrapper.unmount()
  })
  it('makes no price write for unchanged metadata and aborts unloaded reads', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper.vm.save('test')
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
    const load = vi
      .mocked(modelPricingApi.load)
      .mockImplementation(() => new Promise(() => {}))
    const pending = mount(ModelPricingEditor, {
      props: { modelName: 'test' },
      global: { plugins: [i18n] },
    })
    await expect(pending.vm.validate()).rejects.toThrow(
      'not loaded successfully'
    )
    const signal = load.mock.calls.at(-1)![0]!
    pending.unmount()
    expect(signal.aborted).toBe(true)
  })
})
