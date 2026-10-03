import { describe, expect, it } from 'vitest'

import {
  loginMethodLabel,
  sessionDevice,
  sessionDeviceCategory,
} from '@/utils/loginSession'

const labels = {
  unknown: 'Unknown',
  password: 'Password',
  twoFactor: 'Two-factor Authentication',
  passkey: 'Passkey',
  wechat: 'WeChat',
  telegram: 'Telegram',
  oauth: 'OAuth',
}

describe('login session formatting', () => {
  it('detects browser and operating system from user agents', () => {
    expect(
      sessionDevice(
        'Mozilla/5.0 (Windows NT 10.0) AppleWebKit Chrome/124.0',
        'Unknown device',
        'Browser'
      )
    ).toBe('Chrome · Windows')
    expect(sessionDevice('', 'Unknown device', 'Browser')).toBe(
      'Unknown device'
    )
  })

  it('categorizes device types for icon mapping', () => {
    expect(
      sessionDeviceCategory(
        'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit Chrome/120.0 Mobile'
      )
    ).toBe('mobile')
    expect(
      sessionDeviceCategory(
        'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit'
      )
    ).toBe('mobile')
    expect(
      sessionDeviceCategory(
        'Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit'
      )
    ).toBe('tablet')
    expect(
      sessionDeviceCategory(
        'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit Chrome/124.0'
      )
    ).toBe('desktop')
    expect(sessionDeviceCategory('')).toBe('unknown')
  })

  it('separates tablets from look-alike desktop and phone agents', () => {
    const macDesktop =
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit Safari/605.1'
    // iPadOS desktop mode sends the Mac agent; only touch points tell them apart.
    expect(sessionDeviceCategory(macDesktop, 5)).toBe('tablet')
    expect(sessionDeviceCategory(macDesktop, 0)).toBe('desktop')
    expect(
      sessionDeviceCategory(
        'Mozilla/5.0 (Linux; Android 14; SM-X710) AppleWebKit Chrome/124.0 Safari'
      )
    ).toBe('tablet')
    expect(
      sessionDeviceCategory(
        'Mozilla/5.0 (Windows NT 6.1; Win64; x64; Trident/7.0; Tablet PC 2.0)'
      )
    ).toBe('desktop')
  })

  it('localizes known login methods and preserves custom methods', () => {
    expect(loginMethodLabel('password', labels)).toBe('Password')
    expect(loginMethodLabel('oauth:github', labels)).toBe('OAuth · GitHub')
    expect(loginMethodLabel('custom', labels)).toBe('custom')
  })
})
