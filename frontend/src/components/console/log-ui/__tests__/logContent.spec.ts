import { describe, expect, it } from 'vitest'

import {
  logContentText,
  logTokenName,
} from '@/components/console/log-ui/logContent'
import { mergeServerMessages } from '@/i18n/serverMessages'

mergeServerMessages('zh-CN', {
  'Online top-up succeeded, amount added: {{quota}}':
    '在线充值成功，充值额度：{{quota}}',
  'Payment amount: {{amount}}': '支付金额：{{amount}}',
  'Model test': '模型测试',
})

const topup = {
  content: 'stored English text',
  other: {
    content_parts: [
      {
        key: 'Online top-up succeeded, amount added: {{quota}}',
        params: { quota: '$10.00' },
      },
      { key: 'Payment amount: {{amount}}', params: { amount: '72.00' } },
    ],
  },
}

describe('log content', () => {
  it('renders content_parts in the viewer language', () => {
    expect(logContentText(topup, 'zh-CN')).toBe(
      '在线充值成功，充值额度：$10.00, 支付金额：72.00'
    )
    expect(logContentText(topup, 'en')).toBe(
      'Online top-up succeeded, amount added: $10.00, Payment amount: 72.00'
    )
  })

  it('shows the stored content of rows without valid content_parts', () => {
    const content = '旧版日志内容'
    for (const other of [
      undefined,
      null,
      'not json',
      {},
      { content_parts: [] },
      { content_parts: 'Model test' },
      { content_parts: [{ params: { quota: 1 } }] },
      { content_parts: [{ key: '' }] },
      { content_parts: [{ key: 'Model test', params: 'x' }] },
    ]) {
      expect(logContentText({ content, other }, 'zh-CN')).toBe(content)
    }
  })

  it('translates the token name only for rows written without a token', () => {
    expect(
      logTokenName({ token_id: 0, token_name: 'Model test' }, 'zh-CN')
    ).toBe('模型测试')
    expect(logTokenName({ token_id: 0, token_name: 'Model test' }, 'en')).toBe(
      'Model test'
    )
    expect(
      logTokenName({ token_id: 9, token_name: 'Model test' }, 'zh-CN')
    ).toBe('Model test')
    expect(logTokenName({ token_name: 'Model test' }, 'zh-CN')).toBe(
      'Model test'
    )
  })
})
