import { describe, expect, it } from 'vitest'
import { parsePricingModels } from '@/api/liveContracts'
import { publicPricingReferences } from '../reference'
import {
  countPricingCatalogModes,
  filterPricingCatalogByMode,
  pricingCatalogMode,
} from '../classification'

function models() {
  return parsePricingModels([
    {
      model_name: 'fixed-expression',
      quota_type: 0,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("request", fixed(0.01))',
    },
    {
      model_name: 'token-expression',
      quota_type: 1,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("tokens", p * 0.25)',
    },
    {
      model_name: 'mixed-expression',
      quota_type: 1,
      billing_mode: 'tiered_expr',
      billing_expr:
        'len < 10 ? tier("request", fixed(0.01)) : tier("tokens", p * 0.25)',
    },
    {
      model_name: 'task-expression',
      quota_type: 0,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("request", fixed(0.01))',
      billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
    },
    {
      model_name: 'invalid-expression',
      quota_type: 1,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("broken", p * )',
    },
    {
      model_name: 'unconfigured',
      quota_type: 0,
      billing_mode: 'unconfigured',
      model_ratio: 0,
    },
  ])
}

describe('public pricing categories', () => {
  it('keeps ordinary pricing for mixed provider and task models with a task schema', () => {
    const base = {
      model_name: 'mixed-provider-task',
      quota_type: 0,
      billing_mode: 'tiered_expr',
      billing_expr: 'tier("tokens", p * 0.25)',
      billing_usage_schema: { seconds: { type: 'number', unit: 'second' } },
    }
    const [ordinary, task] = parsePricingModels([
      { ...base, ordinary_pricing: true },
      { ...base, ordinary_pricing: false },
    ])
    expect(pricingCatalogMode(ordinary!)).toBe('token')
    expect(publicPricingReferences(ordinary!)).toMatchObject([
      { key: 'input', amount: '0.25', unit: 'per_million_tokens' },
    ])
    expect(pricingCatalogMode(task!)).toBe('task')
    expect(publicPricingReferences(task!)).toEqual([])
  })

  it('classifies request, token, mixed and task expressions from parsed data', () => {
    const catalog = models()
    expect(catalog.map((model) => pricingCatalogMode(model))).toEqual([
      'request',
      'token',
      'token',
      'task',
      'request',
      'unconfigured',
    ])
  })

  it('uses the same classification for displayed counts and filtered rows', () => {
    const catalog = models()
    expect(countPricingCatalogModes(catalog)).toEqual({
      task: 1,
      request: 2,
      token: 2,
      unconfigured: 1,
    })
    expect(
      filterPricingCatalogByMode(catalog, 'request').map(
        (model) => model.model_name
      )
    ).toEqual(['fixed-expression', 'invalid-expression'])
    expect(filterPricingCatalogByMode(catalog, '')).toEqual(catalog)
  })

  it('keeps pure fixed tiers per request when token counts only select tiers', () => {
    const [model] = parsePricingModels([
      {
        model_name: 'tiered-image',
        quota_type: 0,
        billing_mode: 'tiered_expr',
        billing_expr:
          '(p < 10 ? tier("small", fixed(0.01)) : tier("large", fixed(0.02))) * image_count',
      },
    ])
    expect(pricingCatalogMode(model!)).toBe('request')
  })
})
