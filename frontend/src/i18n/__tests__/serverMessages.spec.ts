import { describe, expect, it } from 'vitest'

import {
  interpolateServerMessage,
  loadServerMessages,
  mergeServerMessages,
  renderServerMessage,
  serverResponseMessage,
  translateServerText,
} from '@/i18n/serverMessages'

const catalogFiles = import.meta.glob<Record<string, unknown>>(
  '../locales/zh-CN/server-messages/*.json',
  { eager: true, import: 'default' }
)

mergeServerMessages('zh-CN', {
  'Quota exceeds the limit of {{max}}': '额度超过上限 {{max}}',
  'Task timed out': '任务超时',
})

function placeholders(text: string): string[] {
  return [...text.matchAll(/\{\{(\w+)\}\}/g)].map((match) => match[1]!).sort()
}

describe('server message rendering', () => {
  it('fills placeholders in one pass and keeps unknown ones', () => {
    expect(
      interpolateServerMessage('Channel {{name}} (#{{id}}) {{missing}}', {
        name: '{{id}}',
        id: 7,
      })
    ).toBe('Channel {{id}} (#7) {{missing}}')
    expect(interpolateServerMessage('Plain {{value}}')).toBe('Plain {{value}}')
  })

  it('translates a known key with params in zh-CN only', () => {
    const params = { max: 100 }
    const key = 'Quota exceeds the limit of {{max}}'
    expect(
      renderServerMessage(
        'zh-CN',
        key,
        params,
        'Quota exceeds the limit of 100'
      )
    ).toBe('额度超过上限 100')
    expect(
      renderServerMessage('en', key, params, 'Quota exceeds the limit of 100')
    ).toBe('Quota exceeds the limit of 100')
  })

  it('shows the English text for a key without a translation', () => {
    expect(
      renderServerMessage(
        'zh-CN',
        'Channel {{id}} does not exist',
        { id: 7 },
        'Channel 7 does not exist'
      )
    ).toBe('Channel 7 does not exist')
    expect(
      renderServerMessage('zh-CN', 'Channel {{id}} does not exist', { id: 7 })
    ).toBe('Channel 7 does not exist')
  })

  it('looks up fixed sentences exactly and passes other text through', () => {
    expect(translateServerText('zh-CN', 'Task timed out')).toBe('任务超时')
    expect(translateServerText('en', 'Task timed out')).toBe('Task timed out')
    const provider =
      'upstream said {{bad}} @user | {raw} and $t(Task timed out)'
    expect(translateServerText('zh-CN', provider)).toBe(provider)
    expect(translateServerText('zh-CN', 'constructor')).toBe('constructor')
    expect(translateServerText('zh-CN', 'toString')).toBe('toString')
  })

  it('reads message_key and message_params from a response', () => {
    expect(
      serverResponseMessage('zh-CN', {
        success: false,
        message: 'Quota exceeds the limit of 5',
        message_key: 'Quota exceeds the limit of {{max}}',
        message_params: { max: 5 },
      })
    ).toBe('额度超过上限 5')
    expect(
      serverResponseMessage('zh-CN', { success: false, message: '原始错误' })
    ).toBe('原始错误')
    expect(
      serverResponseMessage('zh-CN', {
        message: 'Quota exceeds the limit of {{max}}',
        message_key: 'Quota exceeds the limit of {{max}}',
        message_params: 'not an object',
      })
    ).toBe('额度超过上限 {{max}}')
    expect(serverResponseMessage('zh-CN', { success: false })).toBeUndefined()
    expect(serverResponseMessage('zh-CN', 'plain text')).toBeUndefined()
  })
})

describe('server message catalogs', () => {
  it('keeps every catalog file a flat map with matching placeholders', () => {
    const seen = new Map<string, string>()
    for (const [file, catalog] of Object.entries(catalogFiles)) {
      expect(catalog, file).toBeTypeOf('object')
      for (const [key, value] of Object.entries(catalog)) {
        expect(key.trim(), `${file}: empty key`).not.toBe('')
        expect(value, `${file}: ${key}`).toBeTypeOf('string')
        expect(placeholders(value as string), `${file}: ${key}`).toEqual(
          placeholders(key)
        )
        const previous = seen.get(key)
        if (previous !== undefined) {
          expect(value, `${file} redefines ${key}`).toBe(previous)
        }
        seen.set(key, value as string)
      }
    }
  })

  it('loads every catalog file for zh-CN', async () => {
    await loadServerMessages('zh-CN')
    for (const catalog of Object.values(catalogFiles)) {
      for (const [key, value] of Object.entries(catalog)) {
        expect(translateServerText('zh-CN', key)).toBe(value)
      }
    }
  })
})
