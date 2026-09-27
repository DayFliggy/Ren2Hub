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
import { automaticPricingApi } from '@/api/automaticPricing'
import { ApiError } from '@/api/types'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const price: PriceVersion = {
  contract_version: 3,
  calculation_version: 'billing-v3',
  result_unit: 'usd',
  id: 3,
  model: 'test',
  mode: 'unit_price',
  prices: [{ quantity: 'p', unit: 'per_million_tokens', price: '1' }],
  created_at: 1,
  hash: 'hash',
  provenance: { kind: 'retail', retail_reason: 'Reviewed retail price' },
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => vi.restoreAllMocks())
beforeEach(() => {
  vi.spyOn(automaticPricingApi, 'status').mockResolvedValue({
    config: { enabled: true, check_interval_minutes: 30 },
    sources: [],
    origins: [
      {
        model: 'test',
        version_id: 3,
        origin: 'manual',
        source_model: '',
        source_hash: '',
      },
    ],
  })
  vi.spyOn(modelPricingApi, 'taskSchema').mockResolvedValue({
    model: 'test',
    plugin_key: '',
    usage_schema: null,
    manifest_schema: null,
    review_required: false,
    plugins: [],
  })
})
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
  it('edits the selected channel version without overwriting another scope and retains structured prices', async () => {
    const scoped: PriceVersion = {
      ...price,
      id: 7,
      scope_kind: 'channel_model',
      channel_id: 9,
      flat_fee: '0.001',
      effective_from: '2026-01-01T00:00:00Z',
      effective_to: '2027-01-01T00:00:00Z',
      prices: [
        {
          quantity: 'reasoning',
          unit: 'per_million_tokens',
          price: '',
          tier_mode: 'graduated',
          tiers: [
            { up_to: '1000000', unit_price: '1.000000000000000001' },
            { unit_price: '0.5' },
          ],
        },
      ],
    }
    vi.spyOn(modelPricingApi, 'load').mockResolvedValue([scoped, price])
    const publish = vi
      .spyOn(modelPricingApi, 'publish')
      .mockResolvedValue({ ...scoped, id: 8 })
    const wrapper = mount(ModelPricingEditor, {
      props: { modelName: 'test' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('input[inputmode=decimal]').element.value
    ).toBe('1')
    await wrapper.get('[data-price-scope]').setValue('channel_model')
    await wrapper.get('[data-price-channel]').setValue(9)
    await flushPromises()
    expect(
      JSON.parse(
        wrapper.get<HTMLTextAreaElement>('[data-price-structure]').element.value
      )
    ).toEqual(scoped.prices)
    await wrapper.get('[data-price-flat-fee]').setValue('0.002')
    await wrapper.vm.save('test')
    expect(publish).toHaveBeenCalledWith(
      expect.objectContaining({
        scope_kind: 'channel_model',
        channel_id: 9,
        prices: scoped.prices,
        flat_fee: '0.002',
        effective_to: scoped.effective_to,
      }),
      7,
      undefined
    )
    await wrapper.get('[data-price-structure]').setValue('[{"quantity":')
    await expect(wrapper.vm.save('test')).rejects.toThrow()
    expect(publish).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('retains a frozen embedding scope through reference import, conversion, editing and a renamed copy', async () => {
    const reference: PriceVersion = {
      ...price,
      id: 0,
      usage_scope: 'embedding',
    }
    vi.spyOn(modelPricingApi, 'seed').mockResolvedValue({
      prices: [reference],
      unconfigured: [],
      source: 'sub2api:sha',
    })
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper
      .findAll('button')
      .find(
        (button) => button.text() === 'Compare with live reference catalog'
      )!
      .trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-price-conversion-preview]').text()).toContain(
      'Embedding requests only'
    )
    await wrapper.get('[data-apply-price-conversion]').trigger('click')
    expect(wrapper.get('[data-price-usage-scope]').text()).toContain(
      'Embedding requests only'
    )
    await wrapper
      .get('input[inputmode=decimal]')
      .setValue('0.025000000000000001')
    await wrapper
      .findAll('button')
      .find(
        (button) => button.text() === 'Convert unit prices to expression draft'
      )!
      .trigger('click')
    await wrapper.get('[data-apply-price-conversion]').trigger('click')
    await wrapper.vm.save('embedding-copy')
    expect(publish).toHaveBeenCalledWith(
      expect.objectContaining({
        model: 'embedding-copy',
        usage_scope: 'embedding',
        expression: 'v1:p * 0.025000000000000001',
      }),
      0,
      undefined
    )
    wrapper.unmount()
  })

  it('loads a persisted embedding restriction without marking the unchanged price dirty', async () => {
    vi.spyOn(modelPricingApi, 'load').mockResolvedValue([
      { ...price, usage_scope: 'embedding' },
    ])
    const publish = vi
      .spyOn(modelPricingApi, 'publish')
      .mockResolvedValue(price)
    const wrapper = mount(ModelPricingEditor, {
      props: { modelName: 'test' },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.get('[data-price-usage-scope]').text()).toContain(
      'Embedding requests only'
    )
    await wrapper.vm.save('test')
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('compares the current reference catalog before explicitly applying and publishing a manual draft', async () => {
    const reference = {
      ...price,
      id: 0,
      prices: [{ ...price.prices[0]!, price: '2.000000000000000001' }],
    }
    vi.spyOn(modelPricingApi, 'seed').mockResolvedValue({
      prices: [reference],
      unconfigured: [],
      source: 'sub2api:sha-a,axonhub:sha-b',
    })
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper
      .findAll('button')
      .find(
        (button) => button.text() === 'Compare with live reference catalog'
      )!
      .trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('sub2api:sha-a,axonhub:sha-b')
    expect(
      wrapper.get<HTMLInputElement>('input[inputmode=decimal]').element.value
    ).toBe('1')
    expect(wrapper.get('[data-price-conversion-preview]').text()).toContain(
      '2.000000000000000001'
    )
    expect(publish).not.toHaveBeenCalled()
    await wrapper.get('[data-apply-price-conversion]').trigger('click')
    await wrapper.vm.save('test')
    expect(publish).toHaveBeenCalledWith(
      expect.objectContaining({ prices: reference.prices }),
      3,
      undefined
    )
    expect(wrapper.get('[data-price-origin]').text()).toContain('Manual')
    wrapper.unmount()
  })
  it('restores a manual price only after confirmation with the loaded version', async () => {
    const restored = {
      ...price,
      id: 4,
      prices: [{ ...price.prices[0]!, price: '2.000000000000000001' }],
    }
    const restore = vi
      .spyOn(automaticPricingApi, 'restore')
      .mockResolvedValue(restored)
    const { wrapper, publish } = setup()
    await flushPromises()
    expect(wrapper.get('[data-price-origin]').text()).toContain('Manual')
    await wrapper.get('input[inputmode=decimal]').setValue('7')
    await wrapper.get('[data-restore-automatic]').trigger('click')
    expect(restore).not.toHaveBeenCalled()
    vi.mocked(automaticPricingApi.status).mockResolvedValue({
      config: { enabled: true, check_interval_minutes: 30 },
      sources: [],
      origins: [
        {
          model: 'test',
          version_id: 4,
          origin: 'sub2api',
          source_model: 'test',
          source_hash: 'source-sha',
        },
      ],
    })
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(restore).toHaveBeenCalledWith('test', 3)
    expect(
      wrapper.get<HTMLInputElement>('input[inputmode=decimal]').element.value
    ).toBe('2.000000000000000001')
    expect(wrapper.get('[data-price-origin]').text()).toContain('Sub2API')
    expect(wrapper.find('[data-restore-automatic]').exists()).toBe(false)
    await wrapper.vm.save('test')
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('preserves a manual draft after a restore conflict without retrying or publishing', async () => {
    const restore = vi
      .spyOn(automaticPricingApi, 'restore')
      .mockRejectedValue(new ApiError('conflict', { status: 409 }))
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper.get('input[inputmode=decimal]').setValue('7')
    await wrapper.get('[data-restore-automatic]').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(restore).toHaveBeenCalledTimes(1)
    expect(
      wrapper.get<HTMLInputElement>('input[inputmode=decimal]').element.value
    ).toBe('7')
    expect(wrapper.text()).toContain('changed elsewhere')
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('removes a saved usage schema only after explicitly choosing a single fixed per-call price', async () => {
    const savedSchema = {
      version: 1 as const,
      hash: 'old',
      fields: {
        credits: {
          type: 'number' as const,
          unit: 'credit',
          source: 'completion_fact' as const,
          required: true,
        },
      },
    }
    const existing: PriceVersion = {
      ...price,
      prices: [{ quantity: 'credits', unit: 'credit', price: '0.01' }],
      usage_schema: savedSchema,
    }
    const { wrapper, publish } = setup()
    vi.mocked(modelPricingApi.load).mockResolvedValue([existing])
    vi.mocked(modelPricingApi.taskSchema).mockResolvedValue({
      model: 'test',
      plugin_key: 'vidu',
      usage_schema: null,
      manifest_schema: {},
      review_required: true,
      plugins: [{ key: 'vidu', name: 'Vidu', version: '1' }],
    })
    await wrapper.setProps({ open: false })
    await wrapper.setProps({ open: true })
    await flushPromises()
    expect(wrapper.find('[data-fixed-call-without-schema]').exists()).toBe(
      false
    )
    await wrapper.get('input[list="billing-quantities-0"]').setValue('calls')
    await wrapper.get('select').setValue('unit_price')
    const unitSelect = wrapper
      .findAll('select')
      .find((select) => select.find('option[value="per_call"]').exists())!
    await unitSelect.setValue('per_call')
    await wrapper.get('input[inputmode="decimal"]').setValue('0.12')
    expect(wrapper.text()).toContain('credits')
    expect(publish).not.toHaveBeenCalled()
    await wrapper.get('[data-fixed-call-without-schema]').trigger('click')
    await wrapper.vm.save('test')
    expect(publish).toHaveBeenLastCalledWith(
      expect.objectContaining({
        mode: 'unit_price',
        prices: [{ quantity: 'calls', unit: 'per_call', price: '0.12' }],
        usage_schema: undefined,
      }),
      3,
      'vidu'
    )
    expect(existing.usage_schema).toEqual(savedSchema)
    wrapper.unmount()
  })
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
        base_usd: '0.001',
        adjustments: [],
        total_usd: '0.001',
        unrounded_quota: '500',
        rounding_delta: '0',
        calculation_version: 'billing-v3',
        rounding_version: 'half-up-positive-minimum-v1',
      },
    })
    await flushPromises()
    await wrapper.get('[data-price-preview]').trigger('click')
    await flushPromises()
    expect(preview).toHaveBeenCalledWith(
      expect.objectContaining({
        channel_ratio: '1',
        price: expect.objectContaining({ contract_version: 3 }),
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
          base_usd: '0.001',
          adjustments: [],
          total_usd: '0.001',
          unrounded_quota: '500',
          rounding_delta: '0',
          calculation_version: 'billing-v3',
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
