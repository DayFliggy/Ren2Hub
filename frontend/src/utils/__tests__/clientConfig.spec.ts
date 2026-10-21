import { describe, expect, it } from 'vitest'
import { buildClientConfig, clientConfigBaseURL } from '@/utils/clientConfig'
describe('client configuration serialization', () => {
  it.each([
    'javascript:alert(1)',
    'https://user:pass@example.test',
    'https://example.test/?key=value',
    'https://example.test/#secret',
    'https://example.test/\\evil',
    'https://example.test/\n',
  ])('rejects an unsafe address %s', (address) => {
    expect(() => clientConfigBaseURL(address)).toThrow()
  })
  it('preserves reverse proxy prefixes and does not duplicate v1', () => {
    expect(clientConfigBaseURL('https://example.test/gateway/')).toBe(
      'https://example.test/gateway/v1'
    )
    expect(clientConfigBaseURL('https://example.test/gateway/v1/')).toBe(
      'https://example.test/gateway/v1'
    )
  })
  it('requires explicit Pi capabilities and omits unknown limits and prices', () => {
    expect(() =>
      buildClientConfig('pi', 'https://example.test', 'fixture')
    ).toThrow()
    const config = buildClientConfig('pi', 'https://example.test', 'fixture', {
      contextWindow: 10000,
      maxTokens: 1000,
      reasoning: false,
      imageInput: false,
    })
    const parsed = JSON.parse(config.content)
    expect(parsed.providers.ren2hub.apiKey).toBe('${REN2HUB_API_KEY}')
    expect(parsed.providers.ren2hub.models[0]).toEqual({
      id: 'fixture',
      name: 'fixture',
      contextWindow: 10000,
      maxTokens: 1000,
      reasoning: false,
      input: ['text'],
    })
    expect(parsed.providers.ren2hub.models[0]).not.toHaveProperty('cost')
  })
})
