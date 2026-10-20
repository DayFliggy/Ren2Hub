import { describe, expect, it } from 'vitest'
import { parseModelMarket } from '@/api/modelMarket'
import { createModelMarketFixture } from '../../../tests/fixtures/modelMarket'

describe('authenticated model market contract', () => {
  it('preserves decimal values, channel identity, endpoint variants and unavailable telemetry', () => {
    const input = createModelMarketFixture()
    input.channels[0]!.ratio = '0.1234567890123456789'
    input.models[0]!.baselines[0]!.values.ModelRatio = '0.1234567890123456789'
    const parsed = parseModelMarket(input)
    expect(parsed.channels[0]!.ratio).toBe('0.1234567890123456789')
    expect(parsed.models[0]!.baselines[0]!.values.ModelRatio).toBe(
      '0.1234567890123456789'
    )
    expect(
      parsed.models.find((model) => model.id === 'video-preview')!.baselines
    ).toMatchObject([
      { plugin_key: 'seconds', supported_endpoint_types: ['openai-video'] },
      { plugin_key: 'fixed', supported_endpoint_types: ['video-task'] },
    ])
    expect(parsed.telemetry).toEqual({
      status: 'unavailable',
      reason: 'channel_metrics_not_collected',
    })
  })

  it('preserves localized usage labels and exact request multipliers', () => {
    const fixture = createModelMarketFixture()
    const price = fixture.models.find((model) => model.id === 'video-preview')!
      .baselines[0]!
    price.usage_schema = {
      seconds: {
        type: 'number',
        unit: 'second',
        unitLabel: { en: 'seconds', 'zh-CN': '秒' },
        description: { en: 'Duration' },
        enum: ['5', '10'],
      },
    }
    price.billing_details = {
      image_count: false,
      audio_text_branches: false,
      request_rules: [
        { condition: 'quality=hd', multiplier: '1.234567890123456789' },
      ],
    }
    const parsed = parseModelMarket(fixture).models.find(
      (model) => model.id === 'video-preview'
    )!.baselines[0]!
    expect(parsed.usage_schema).toEqual(price.usage_schema)
    expect(parsed.billing_details).toEqual(price.billing_details)
  })

  it.each([
    [
      'null usage field',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        Object.assign(input.models[0]!.baselines[0]!, {
          usage_schema: { seconds: null },
        })
      },
    ],
    [
      'invalid localized usage label',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        Object.assign(input.models[0]!.baselines[0]!, {
          usage_schema: { seconds: { unitLabel: { en: 7 } } },
        })
      },
    ],
    [
      'invalid usage choices',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        Object.assign(input.models[0]!.baselines[0]!, {
          usage_schema: { seconds: { enum: [5] } },
        })
      },
    ],
    [
      'numeric request rule multiplier',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.models[0]!.baselines[0]!.billing_details = {
          request_rules: [{ condition: 'quality=hd', multiplier: 1.2 }],
        }
      },
    ],
    [
      'null request rule',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.models[0]!.baselines[0]!.billing_details = {
          request_rules: [null],
        }
      },
    ],
    [
      'invalid image count rule',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.models[0]!.baselines[0]!.billing_details = { image_count: 'true' }
      },
    ],
    [
      'unknown cache-write meaning',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.models[0]!.baselines[0]!.cache_write_mode = 'unknown'
      },
    ],
    [
      'numeric multiplier',
      (input: ReturnType<typeof createModelMarketFixture>) =>
        Object.assign(input.channels[0]!, { ratio: 0.2 }),
    ],
    [
      'negative multiplier',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.channels[0]!.ratio = '-1'
      },
    ],
    [
      'numeric price',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.models[0]!.baselines[0]!.values.ModelRatio = 0.1
      },
    ],
    [
      'missing price on configured row',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.models[0]!.baselines[0]!.values = {}
      },
    ],
    [
      'unavailable channel reference',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.offers[0]!.channel_id = 999
      },
    ],
    [
      'cross-model price reference',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.offers[0]!.pricing_id = input.models[1]!.baselines[0]!.id
      },
    ],
    [
      'unauthorized endpoint',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.offers[0]!.supported_endpoint_types = ['private-endpoint']
      },
    ],
    [
      'duplicate offer',
      (input: ReturnType<typeof createModelMarketFixture>) => {
        input.offers.push({ ...input.offers[0]! })
      },
    ],
    [
      'model-wide metrics masquerading as channel telemetry',
      (input: ReturnType<typeof createModelMarketFixture>) =>
        Object.assign(input.telemetry, { status: 'available' }),
    ],
  ])('rejects %s instead of displaying an invented quote', (_label, mutate) => {
    const fixture = createModelMarketFixture()
    mutate(fixture)
    expect(() => parseModelMarket(fixture)).toThrow()
  })
})
