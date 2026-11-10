import { describe, expect, it } from 'vitest'
import { ApiError } from '@/api/types'
import { isPricingConflict } from '../errors'
import { changedPreviewRows, pricingPreviewRows } from '../previewRows'

const values = (rows: ReturnType<typeof pricingPreviewRows>) =>
  rows.map((row) => [row.key, row.value])

describe('pricing preview rows', () => {
  // Default quota unit: 500000 quota per USD, so ratio 1 is $2 / 1M tokens.
  const token = {
    ModelRatio: 1.5,
    CompletionRatio: 4,
    CacheRatio: 0.1,
    CreateCacheRatio: 1.25,
  }

  it('adds the 1h write row for Claude cache-write pricing', () => {
    expect(values(pricingPreviewRows(token, 'claude_ttl'))).toEqual([
      ['input', '$3.000'],
      ['output', '$12.000'],
      ['cache', '$0.300'],
      ['cacheWrite5m', '$3.750'],
      ['cacheWrite1h', '$6.000'],
    ])
  })

  it('adds the 12h write row for OpenAI cache-write pricing', () => {
    expect(values(pricingPreviewRows(token, 'openai_ttl')).slice(3)).toEqual([
      ['cacheWrite30m', '$3.750'],
      ['cacheWrite12h', '$7.500'],
    ])
  })

  it('hides cache writes the backend does not bill and default ratios', () => {
    const rows = pricingPreviewRows(
      { ...token, CacheRatio: 1, ImageRatio: 1 },
      'none'
    )
    expect(rows.map((row) => row.key)).toEqual(['input', 'output'])
  })

  it('shows a configured write price when no mode is known', () => {
    expect(
      pricingPreviewRows(token).find((row) => row.key === 'cacheWrite')
    ).toMatchObject({ value: '$3.750', unit: 'perMillion' })
  })

  it('describes fixed image prices and backend request rules', () => {
    const rows = pricingPreviewRows({ ModelPrice: 0.04 }, undefined, {
      image_count: true,
      request_rules: [{ condition: 'size == "1024x1792"', multiplier: 2 }],
    })
    expect(rows).toEqual([
      {
        key: 'imagePrice',
        label: 'imagePrice',
        value: '$0.040',
        unit: 'perImage',
        translateLabel: true,
      },
      {
        key: 'imageCountNote',
        label: 'imageCountNote',
        value: '',
        unit: 'none',
        translateLabel: true,
      },
      {
        key: 'rule-0',
        label: 'size == "1024x1792"',
        value: '× 2',
        unit: 'none',
        translateLabel: false,
      },
    ])
  })

  it('prefers the absolute Gemini audio price over the ratio', () => {
    const rows = pricingPreviewRows({ ModelRatio: 1, AudioRatio: 2 }, 'none', {
      audio_input_price: 0.7,
    })
    expect(rows.find((row) => row.key === 'audio')?.value).toBe('$0.700')
  })
})

describe('pricing save conflicts', () => {
  it('counts only a version conflict, not a changed login session', () => {
    expect(isPricingConflict(new ApiError('changed', { status: 409 }))).toBe(
      true
    )
    expect(
      isPricingConflict(
        new ApiError('session', { status: 409, code: 'AUTH_SESSION_CHANGED' })
      )
    ).toBe(false)
    expect(isPricingConflict(new ApiError('bad', { status: 400 }))).toBe(false)
    expect(isPricingConflict(new Error('changed'))).toBe(false)
  })
})

describe('changed price lines', () => {
  const lines = (rows: ReturnType<typeof changedPreviewRows>) =>
    rows.map((row) => [row.key, row.before, row.after])

  it('lists only the displayed prices that change, old next to new', () => {
    expect(
      lines(
        changedPreviewRows(
          { ModelRatio: 1.5, CompletionRatio: 4 },
          { ModelRatio: '1.5', CompletionRatio: '5', CacheRatio: '0.1' }
        )
      )
    ).toEqual([
      ['output', '$12.000', '$15.000'],
      ['cache', '', '$0.300'],
    ])
  })

  it('compares plugin prices by plugin and reports removed lines', () => {
    expect(
      lines(
        changedPreviewRows(
          {
            ModelPrice: 1,
            'billing_setting.plugin_billing_expr': { a: 'u("seconds")' },
          },
          {
            ModelPrice: 1,
            'billing_setting.plugin_billing_expr': { b: 'u("seconds") * 2' },
          }
        )
      )
    ).toEqual([
      ['plugin:b', '', 'u("seconds") * 2'],
      ['plugin:a', 'u("seconds")', ''],
    ])
  })
})
