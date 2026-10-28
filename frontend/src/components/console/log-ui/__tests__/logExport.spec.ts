import { describe, expect, it } from 'vitest'

import {
  getLogExportValues,
  LOG_EXPORT_HEADERS,
} from '@/components/console/log-ui/logExport'
import { mergeServerMessages } from '@/i18n/serverMessages'
import type { LogItem } from '@/types/console'

const log: LogItem = {
  id: 1,
  type: 'consume',
  token_name: 'Export key',
  model: 'gpt-4o',
  channel: 'OpenAI',
  other: JSON.stringify({
    reasoning_effort: 'xhigh',
    cache_creation_tokens_12h: 2,
    cache_tokens: 3,
    cache_write_tokens: 4,
  }),
  prompt_tokens: 1,
  completion_tokens: 2,
  cache_ttl: '5m',
  quota: '3',
  latency: 4.5,
  first_token_latency: 1.25,
  request_mode: 'stream',
  tps: 5,
  content: 'done',
  created: 1_752_000_000,
}

describe('log exports', () => {
  it('exports raw quota and timing precision independently of rounded UI labels', () => {
    const values = getLogExportValues(
      { ...log, quota: '9007199254740993', latency: 1.123456789 },
      'en'
    )
    expect(values[LOG_EXPORT_HEADERS.indexOf('quota')]).toBe('9007199254740993')
    expect(values[LOG_EXPORT_HEADERS.indexOf('latency')]).toBe(1.123456789)
  })
  it('appends 12h cache writes without shifting existing export columns', () => {
    expect(LOG_EXPORT_HEADERS).toEqual([
      'time',
      'token',
      'type',
      'model',
      'channel',
      'reasoning_effort',
      'request_mode',
      'first_token_latency',
      'prompt_tokens',
      'completion_tokens',
      'cache_read_tokens',
      'cache_write_tokens',
      'cache_ttl',
      'latency',
      'tps',
      'quota',
      'content',
      'cache_write_tokens_12h',
    ])
    expect(getLogExportValues(log, 'en')).toEqual([
      expect.any(String),
      'Export key',
      'consume',
      'gpt-4o',
      'OpenAI',
      'xHigh',
      'stream',
      1.25,
      1,
      2,
      3,
      4,
      '5m',
      4.5,
      5,
      '3',
      'done',
      2,
    ])
  })
  it('exports the token name and content in the viewer language', () => {
    mergeServerMessages('zh-CN', {
      'Model test': '模型测试',
      'Test request for {{model}}': '{{model}} 测试请求',
    })
    const values = getLogExportValues(
      {
        ...log,
        token_id: 0,
        token_name: 'Model test',
        other: {
          content_parts: [
            { key: 'Test request for {{model}}', params: { model: 'gpt-4o' } },
          ],
        },
        content: 'Test request for gpt-4o',
      },
      'zh-CN'
    )
    expect(values[LOG_EXPORT_HEADERS.indexOf('token')]).toBe('模型测试')
    expect(values[LOG_EXPORT_HEADERS.indexOf('content')]).toBe(
      'gpt-4o 测试请求'
    )
  })
})
