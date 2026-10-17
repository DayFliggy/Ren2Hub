import { describe, expect, it } from 'vitest'
import { buildClientConfig, clientConfigBaseURL } from '@/utils/clientConfig'

describe('client configuration formats', () => {
  it('exports Responses TOML with an environment reference and escaped model ID', () => {
    const result = buildClientConfig(
      'codex',
      'https://gateway.example/prefix/',
      'new-"model\\revision'
    )
    expect(result.filename).toBe('ren2hub.config.toml')
    expect(result.content).toContain('model = "new-\\"model\\\\revision"')
    expect(result.content).toContain(
      'base_url = "https://gateway.example/prefix/v1"'
    )
    expect(result.content).toContain('env_key = "REN2HUB_API_KEY"')
    expect(result.content).toContain('wire_api = "responses"')
    expect(result.content).not.toMatch(/context_window|cost|api_key\s*=/)
    expect(clientConfigBaseURL('https://gateway.example/v1/')).toBe(
      'https://gateway.example/v1'
    )
  })

  it.each([
    '',
    '/relative',
    'javascript:alert(1)',
    'https://user:secret@gateway.example',
    'https://gateway.example?key=secret',
    'https://gateway.example#fragment',
    'https://gateway.example\n',
    'https:\\gateway.example',
  ])('rejects unsafe or ambiguous server address %s', (address) => {
    expect(() => clientConfigBaseURL(address)).toThrow('invalid_address')
  })

  it('requires explicit Pi capabilities and never invents prices or limits', () => {
    expect(() =>
      buildClientConfig('pi', 'https://gateway.example', 'future-model')
    ).toThrow('pi_settings_required')
    const settings = {
      contextWindow: 64000,
      maxTokens: 8000,
      reasoning: true,
      imageInput: true,
    }
    const result = buildClientConfig(
      'pi',
      'https://gateway.example/v1',
      'future-model',
      settings
    )
    const config = JSON.parse(result.content)
    expect(config.providers.ren2hub).toEqual({
      baseUrl: 'https://gateway.example/v1',
      apiKey: '${REN2HUB_API_KEY}',
      api: 'openai-completions',
      models: [
        {
          id: 'future-model',
          name: 'future-model',
          contextWindow: 64000,
          maxTokens: 8000,
          reasoning: true,
          input: ['text', 'image'],
        },
      ],
    })
    expect(result.filename).toBe('ren2hub.models.json')
    expect(result.content).not.toContain('cost')
    expect(() =>
      buildClientConfig('pi', 'https://gateway.example', 'future-model', {
        ...settings,
        maxTokens: 64001,
      })
    ).toThrow('pi_settings_required')
    expect(() =>
      buildClientConfig('pi', 'https://gateway.example', 'future-model', {
        ...settings,
        contextWindow: 1.5,
      })
    ).toThrow('pi_settings_required')
  })
})
