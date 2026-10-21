export type ClientConfigFormat = 'codex' | 'pi'

export interface ClientConfigCatalog {
  baseURL: string
  models: string[]
  protocolModels: Record<ClientConfigFormat, string[]>
}

/** Explicit client limits supplied by the user, never inferred from a model ID. */
export interface PiModelSettings {
  contextWindow: number
  maxTokens: number
  reasoning: boolean
  imageInput: boolean
}

export interface ClientConfiguration {
  filename: string
  mediaType: string
  content: string
}

export type ClientConfigErrorCode =
  | 'invalid_address'
  | 'no_models'
  | 'no_protocol_models'
  | 'model_unavailable'
  | 'token_unavailable'
  | 'pi_settings_required'

export class ClientConfigError extends Error {
  constructor(public readonly code: ClientConfigErrorCode) {
    super(code)
    this.name = 'ClientConfigError'
  }
}
