import { describe, expect, it } from 'vitest'

import { wechatQRCode } from '@/utils/wechatQRCode'

describe('wechatQRCode', () => {
  it('accepts same-origin and configured external HTTP(S) images', () => {
    expect(wechatQRCode('/assets/wechat.png')).toEqual({
      state: 'ready',
      src: `${window.location.origin}/assets/wechat.png`,
    })
    expect(wechatQRCode(' https://cdn.example.com/qr/wechat.png ')).toEqual({
      state: 'ready',
      src: 'https://cdn.example.com/qr/wechat.png',
    })
  })

  it('reports an empty value as not configured', () => {
    expect(wechatQRCode(undefined)).toEqual({ state: 'missing' })
    expect(wechatQRCode('   ')).toEqual({ state: 'missing' })
  })

  it('reports a rejected protocol as invalid, not as missing', () => {
    expect(wechatQRCode('javascript:alert(1)')).toEqual({ state: 'invalid' })
    expect(wechatQRCode('ftp://files.example.com/wechat.png')).toEqual({
      state: 'invalid',
    })
  })
})
