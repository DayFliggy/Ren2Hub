import { describe, expect, it } from 'vitest'

import { loginMethodLabel, sessionDevice } from '@/utils/loginSession'

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

  it('localizes known login methods and preserves custom methods', () => {
    expect(loginMethodLabel('password', labels)).toBe('Password')
    expect(loginMethodLabel('oauth:github', labels)).toBe('OAuth · GitHub')
    expect(loginMethodLabel('custom', labels)).toBe('custom')
  })
})
