import { api } from './client'
import { ApiError } from './types'

export interface OidcEndpoints {
  'oidc.well_known': string
  'oidc.authorization_endpoint': string
  'oidc.token_endpoint': string
  'oidc.user_info_endpoint': string
}

function isHttpUrl(value: unknown): value is string {
  if (typeof value !== 'string' || !value.trim()) return false
  try {
    return ['http:', 'https:'].includes(new URL(value).protocol)
  } catch {
    return false
  }
}

/** Both built-in options and discovery require RootAuth and option:write. */
export async function discoverOidcEndpoints(
  wellKnown: string
): Promise<OidcEndpoints> {
  const result = await api.post<{
    well_known_url?: unknown
    discovery?: Record<string, unknown>
  }>('/api/custom-oauth-provider/discovery', {
    well_known_url: wellKnown.trim(),
  })
  const endpoints = {
    'oidc.well_known': result?.well_known_url,
    'oidc.authorization_endpoint': result?.discovery?.authorization_endpoint,
    'oidc.token_endpoint': result?.discovery?.token_endpoint,
    'oidc.user_info_endpoint': result?.discovery?.userinfo_endpoint,
  }
  if (!Object.values(endpoints).every(isHttpUrl)) {
    throw new ApiError(
      'OIDC discovery must return valid HTTP or HTTPS authorization, token and user info endpoints.',
      {
        code: 'INVALID_OIDC_DISCOVERY',
      }
    )
  }
  return endpoints as OidcEndpoints
}
