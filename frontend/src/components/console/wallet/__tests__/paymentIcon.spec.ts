import { describe, expect, it } from 'vitest'

import { BRAND_MARKS } from '../brandMarks'
import { paymentIcon } from '../paymentIcon'

describe('payment icons', () => {
  it('renders only plain https images from configuration', () => {
    expect(
      paymentIcon({
        icon: 'https://cdn.example.com/usdt.png',
        fallback: 'card',
      })
    ).toEqual({ kind: 'image', src: 'https://cdn.example.com/usdt.png' })
    for (const icon of [
      'http://cdn.example.com/usdt.png',
      'https://user:pass@cdn.example.com/usdt.png',
      'javascript:alert(1)',
    ])
      expect(paymentIcon({ icon, fallback: 'card' }).kind).not.toBe('image')
  })

  it('resolves built-in identifiers and network aliases before the method type', () => {
    expect(paymentIcon({ icon: 'Telegram', fallback: 'link' })).toEqual({
      kind: 'brand',
      mark: BRAND_MARKS.telegram,
    })
    expect(paymentIcon({ type: 'wxpay', fallback: 'card' })).toEqual({
      kind: 'brand',
      mark: BRAND_MARKS.wechat,
    })
    expect(
      paymentIcon({
        icon: 'unknown-name',
        type: 'gmpay:usdt.bsc',
        fallback: 'card',
      })
    ).toEqual({ kind: 'brand', mark: BRAND_MARKS.bnbchain })
  })

  it('tells crypto methods apart by network, with a letter badge for unknown chains', () => {
    expect(
      paymentIcon({ type: 'gmpay:usdt.tron', fallback: 'card' })
    ).toMatchObject({ kind: 'letter', letter: 'T' })
    expect(paymentIcon({ type: 'gmpay:usdc.base', fallback: 'card' })).toEqual({
      kind: 'brand',
      mark: BRAND_MARKS.base,
    })
    expect(
      paymentIcon({ type: 'gmpay:usdt.kaia', fallback: 'card' })
    ).toMatchObject({ kind: 'letter', letter: 'K' })
  })

  it('falls back to a generic mark when nothing matches', () => {
    expect(paymentIcon({ type: 'creem', fallback: 'card' })).toEqual({
      kind: 'generic',
      name: 'bank',
    })
    expect(paymentIcon({ type: 'custom', fallback: 'card' })).toEqual({
      kind: 'generic',
      name: 'card',
    })
    expect(paymentIcon({ icon: 'unknown-name', fallback: 'link' })).toEqual({
      kind: 'generic',
      name: 'link',
    })
  })
})
