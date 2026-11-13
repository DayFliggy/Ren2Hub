import { describe, expect, it } from 'vitest'
import {
  evaluatePasswordStrength,
  getPasswordConfirmationState,
} from '../password-strength'
import { isAccountPasswordLengthValid } from '../password-policy'
describe('password feedback', () => {
  it('blocks only Unicode character lengths outside 8–128', () => {
    for (const password of [
      'password',
      '11111111',
      'abcdefgh',
      '密码'.repeat(4),
      '😀'.repeat(128),
    ]) {
      expect(isAccountPasswordLengthValid(password)).toBe(true)
      expect(evaluatePasswordStrength(password).meetsRequirements).toBe(true)
    }
    for (const password of ['a'.repeat(7), '😀'.repeat(129)])
      expect(evaluatePasswordStrength(password).meetsRequirements).toBe(false)
  })
  it('marks whole common passwords, repeats and wrapped runs weak without penalizing a long phrase containing a common word', () => {
    for (const password of [
      'Password123!',
      '12345678901234567890',
      'a'.repeat(24),
    ])
      expect(evaluatePasswordStrength(password).score).toBe(1)
    expect(evaluatePasswordStrength('administrator forest lake').score).toBe(4)
    expect(evaluatePasswordStrength('correct horse battery staple').score).toBe(
      4
    )
  })
  it('shows mismatch only after confirmation has input', () => {
    expect(getPasswordConfirmationState('secret', '')).toBe('empty')
    expect(getPasswordConfirmationState('secret', 'other')).toBe('mismatch')
    expect(getPasswordConfirmationState('secret', 'secret')).toBe('match')
  })
})
