import { describe, expect, it } from 'vitest'

import { resolveModelProvider } from '../modelProvider'

describe('model provider identification', () => {
  it.each([
    'meta/muse-spark-1.3-contributor',
    'muse-spark-1.3',
    'meta/muse-spark-1.2',
    ' META/MUSE-SPARK-1.3 ',
    'meta/custom',
  ])('recognizes %s as Meta instead of the Spark fallback', (model) => {
    expect(resolveModelProvider(model)).toMatchObject({
      name: 'Meta',
      icon: 'Meta.Color',
    })
  })

  it.each(['spark', 'sparkdesk-v3.5', 'iflytek/spark'])(
    'keeps %s on iFlytek',
    (model) => {
      expect(resolveModelProvider(model)).toMatchObject({
        name: 'iFlytek',
        icon: 'Spark.Color',
      })
    }
  )
})
