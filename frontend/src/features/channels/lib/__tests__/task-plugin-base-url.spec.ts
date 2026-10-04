import { describe, expect, it } from 'vitest'

import {
  assessBaseUrlTrust,
  nextTaskPluginBaseUrl,
} from '../task-plugin-base-url'

describe('task plugin channel base URL', () => {
  it('fills the plugin default only over an empty or previous default value', () => {
    expect(nextTaskPluginBaseUrl('', undefined, 'https://a.example')).toBe(
      'https://a.example'
    )
    expect(
      nextTaskPluginBaseUrl(
        'https://a.example/',
        'https://a.example',
        'https://b.example'
      )
    ).toBe('https://b.example')
    // A hand-typed address survives switching plugins.
    expect(
      nextTaskPluginBaseUrl(
        'https://mine.example',
        'https://a.example',
        'https://b.example'
      )
    ).toBeNull()
    expect(nextTaskPluginBaseUrl('', 'https://a.example', undefined)).toBeNull()
  })

  it('flags plain HTTP and private hosts without blocking them', () => {
    expect(assessBaseUrlTrust('https://api.example.com')).toEqual({
      plainHttp: false,
      privateHost: false,
    })
    expect(assessBaseUrlTrust('http://10.0.0.5:8080')).toEqual({
      plainHttp: true,
      privateHost: true,
    })
    expect(assessBaseUrlTrust('https://169.254.169.254')?.privateHost).toBe(
      true
    )
    expect(assessBaseUrlTrust('https://upstream')?.privateHost).toBe(true)
    expect(assessBaseUrlTrust('not a url')).toBeNull()
  })
})
