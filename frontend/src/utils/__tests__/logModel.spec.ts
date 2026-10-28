import { describe, expect, it } from 'vitest'
import type { LogItem } from '@/types/console'
import { logModelObservation } from '../logModel'

describe('observed response model comparison', () => {
  it.each([
    ['gpt-4o', 'gpt-4o-2026-01-01', false],
    ['gpt-4o', 'Provider/GPT-4O', false],
    ['alias', 'provider-model-2026-01-01', false],
    ['alias', 'unrelated-model', true],
    ['alias', ' ', false],
  ])('compares %s against %s', (model, response, mismatch) => {
    expect(
      logModelObservation({
        model,
        other: JSON.stringify({
          upstream_model_name: 'provider-model',
          response_model: {
            requested_model: model,
            upstream_model: 'provider-model',
            returned_model: response,
          },
        }),
      } as LogItem).mismatch
    ).toBe(mismatch)
  })
  it('does not infer response models from mappings or malformed historic evidence', () => {
    expect(
      logModelObservation({
        model: 'alias',
        other: { upstream_model_name: 'different' },
      } as unknown as LogItem)
    ).toMatchObject({ response: '', mismatch: false })
    expect(
      logModelObservation({ model: 'alias', other: 'invalid' } as LogItem)
        .mismatch
    ).toBe(false)
    expect(
      logModelObservation({
        model: 'alias',
        other: { response_model_name: 'unrelated-model' },
      } as unknown as LogItem)
    ).toMatchObject({ response: '', mismatch: false })
  })
})
