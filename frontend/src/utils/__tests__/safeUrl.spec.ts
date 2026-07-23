import { describe, expect, it } from 'vitest'

import { safeExternalUrl, safeImageUrl } from '@/utils/safeUrl'

describe('safe URL helpers', () => {
  it('allows same-origin URLs and explicitly trusted external origins', () => {
    expect(safeExternalUrl('/docs')).toBe('http://localhost:3000/docs')
    expect(safeExternalUrl('https://docs.example.com/guide')).toBeNull()
    expect(
      safeExternalUrl('https://docs.example.com/guide', [
        'https://docs.example.com',
      ])
    ).toBe('https://docs.example.com/guide')
  })

  it('applies the same origin policy to images and blob URLs', () => {
    expect(safeImageUrl('https://images.example.com/cover.webp')).toBeNull()
    expect(
      safeImageUrl('https://images.example.com/cover.webp', [
        'https://images.example.com/gallery',
      ])
    ).toBe('https://images.example.com/cover.webp')
    expect(safeImageUrl('blob:https://images.example.com/asset')).toBeNull()
    expect(safeImageUrl('blob:http://localhost:3000/asset')).toBe(
      'blob:http://localhost:3000/asset'
    )
  })

  it('rejects executable and malformed protocols', () => {
    expect(safeExternalUrl('javascript:alert(1)')).toBeNull()
    expect(safeExternalUrl('data:text/html,hello')).toBeNull()
  })

  it('accepts raster data images but rejects SVG data payloads', () => {
    expect(safeImageUrl('data:image/png;base64,aGVsbG8=')).toContain(
      'image/png'
    )
    expect(safeImageUrl('data:image/svg+xml,<svg onload=alert(1)>')).toBeNull()
  })
})
