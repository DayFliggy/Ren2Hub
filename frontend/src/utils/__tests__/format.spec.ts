import { describe, expect, it } from 'vitest'

import { maskKey } from '@/utils/format'

describe('maskKey', () => {
  it('never returns a non-empty secret verbatim', () => {
    expect(maskKey('')).toBe('')
    expect(maskKey('a')).toBe('•')
    expect(maskKey('sk-abcdefgh')).toBe('sk-••••gh')
    expect(maskKey('secret')).not.toBe('secret')
    expect(maskKey('secret')).not.toContain('secret')
  })

  it('keeps the established preview shape for normal token keys', () => {
    expect(maskKey('sk-7YX12345678lpK')).toBe('sk-7YX••••8lpK')
  })
})
