import {
  ClientConfigError,
  type ClientConfigFormat,
  type ClientConfiguration,
  type PiModelSettings,
} from '@/types/clientConfig'

function unsafeText(value: string): boolean {
  return Array.from(value).some((character) => {
    const code = character.codePointAt(0)!
    return code < 32 || code === 127 || (code >= 0xd800 && code <= 0xdfff)
  })
}

export function clientConfigBaseURL(address: string): string {
  let url: URL
  try {
    url = new URL(address)
  } catch {
    throw new ClientConfigError('invalid_address')
  }
  if (
    !['http:', 'https:'].includes(url.protocol) ||
    !url.hostname ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    /[\s\\]/u.test(address) ||
    unsafeText(address)
  ) {
    throw new ClientConfigError('invalid_address')
  }
  const base = url.href.replace(/\/+$/, '')
  return base.endsWith('/v1') ? base : `${base}/v1`
}

export function validPiSettings(value: PiModelSettings | undefined): boolean {
  return Boolean(
    value &&
    Number.isSafeInteger(value.contextWindow) &&
    value.contextWindow > 0 &&
    Number.isSafeInteger(value.maxTokens) &&
    value.maxTokens > 0 &&
    value.maxTokens <= value.contextWindow &&
    typeof value.reasoning === 'boolean' &&
    typeof value.imageInput === 'boolean'
  )
}

export function buildClientConfig(
  format: ClientConfigFormat,
  baseURL: string,
  model: string,
  pi?: PiModelSettings
): ClientConfiguration {
  if (!model || model.length > 256 || unsafeText(model))
    throw new ClientConfigError('model_unavailable')
  const base = clientConfigBaseURL(baseURL)
  if (format === 'codex') {
    // JSON basic strings also escape TOML quotes, backslashes and controls.
    return {
      filename: 'ren2hub.config.toml',
      mediaType: 'application/toml',
      content: [
        `model = ${JSON.stringify(model)}`,
        'model_provider = "ren2hub"',
        '',
        '[model_providers.ren2hub]',
        'name = "Ren2Hub"',
        `base_url = ${JSON.stringify(base)}`,
        'env_key = "REN2HUB_API_KEY"',
        'wire_api = "responses"',
        '',
      ].join('\n'),
    }
  }
  if (!validPiSettings(pi)) throw new ClientConfigError('pi_settings_required')
  return {
    filename: 'ren2hub.models.json',
    mediaType: 'application/json',
    content:
      JSON.stringify(
        {
          providers: {
            ren2hub: {
              baseUrl: base,
              apiKey: '${REN2HUB_API_KEY}',
              api: 'openai-completions',
              models: [
                {
                  id: model,
                  name: model,
                  contextWindow: pi!.contextWindow,
                  maxTokens: pi!.maxTokens,
                  reasoning: pi!.reasoning,
                  input: pi!.imageInput ? ['text', 'image'] : ['text'],
                },
              ],
            },
          },
        },
        null,
        2
      ) + '\n',
  }
}
