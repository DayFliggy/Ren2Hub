import { getAffiliateAttribution } from '@/utils/affiliate'
import { api } from './client'
import type { PublicStatus } from './public'
import type { SecurityOperation } from './security'
import { proofOptions } from './security'
import { safeExternalUrl } from '@/utils/safeUrl'
import { ClientError } from './clientError'
import { ApiError } from './types'

export function oidcDisplayName(status: PublicStatus): string {
  return status.oidc_display_name?.trim() || 'OIDC'
}

export function oauthProviders(
  status: PublicStatus
): Array<{ slug: string; name: string }> {
  return [
    ...(status.github_oauth ? [{ slug: 'github', name: 'GitHub' }] : []),
    ...(status.discord_oauth ? [{ slug: 'discord', name: 'Discord' }] : []),
    ...(status.linuxdo_oauth ? [{ slug: 'linuxdo', name: 'Linux DO' }] : []),
    ...(status.oidc_enabled
      ? [{ slug: 'oidc', name: oidcDisplayName(status) }]
      : []),
    ...(status.telegram_oauth && status.telegram_oauth_configured
      ? [{ slug: 'telegram', name: 'Telegram' }]
      : []),
    ...(status.custom_oauth_providers ?? []).map(({ slug, name }) => ({
      slug,
      name,
    })),
  ]
}

export async function completeOAuth<T>(
  status: PublicStatus,
  provider: string,
  intent: 'login' | 'bind' | 'verify',
  operation?: SecurityOperation,
  proof?: string,
  signal?: AbortSignal,
  reservedPopup?: Window
): Promise<T> {
  const popup =
    reservedPopup ??
    window.open('', 'ren2hub-oauth', 'popup,width=560,height=720')
  if (!popup) throw new ClientError('oauthPopupBlocked')
  try {
    const flow = await api.post<{
      flow_token: string
      authorization_url?: string
    }>(
      '/api/oauth/state',
      {
        provider,
        intent,
        ...operation,
        ...(intent === 'login' && getAffiliateAttribution()
          ? { aff: getAffiliateAttribution() }
          : {}),
      },
      proof ? proofOptions(proof) : undefined
    )
    if (signal?.aborted) throw new ClientError('oauthCancelled', 'AbortError')
    let authorizationURL = flow.authorization_url
    if (!authorizationURL) {
      const custom = status.custom_oauth_providers?.find(
        (item) => item.slug === provider
      )
      const settings: Record<
        string,
        [string | undefined, string | undefined, string]
      > = {
        github: [
          status.github_client_id,
          'https://github.com/login/oauth/authorize',
          'user:email',
        ],
        discord: [
          status.discord_client_id,
          'https://discord.com/api/oauth2/authorize',
          'identify email',
        ],
        linuxdo: [
          status.linuxdo_client_id,
          'https://connect.linux.do/oauth2/authorize',
          '',
        ],
        oidc: [
          status.oidc_client_id,
          status.oidc_authorization_endpoint,
          'openid profile email',
        ],
      }
      const [client, endpoint, scopes] = settings[provider] ?? [
        custom?.client_id,
        custom?.authorization_endpoint,
        custom?.scopes ?? '',
      ]
      if (!client || !endpoint)
        throw new ClientError('oauthProviderUnavailable')
      const url = new URL(endpoint)
      url.searchParams.set('client_id', client)
      url.searchParams.set(
        'redirect_uri',
        `${window.location.origin}/oauth/${provider}`
      )
      url.searchParams.set('response_type', 'code')
      url.searchParams.set('state', flow.flow_token)
      if (scopes) url.searchParams.set('scope', scopes)
      authorizationURL = url.toString()
    }
    const safeURL = safeExternalUrl(authorizationURL, [
      new URL(authorizationURL).origin,
    ])
    if (!safeURL) throw new ClientError('oauthInvalidAuthorizationUrl')
    return await new Promise<T>((resolve, reject) => {
      let processing = false
      const deadline = Date.now() + 5 * 60_000
      const finish = () => {
        window.removeEventListener('message', listener)
        window.removeEventListener('beforeunload', cancel)
        signal?.removeEventListener('abort', cancel)
        window.clearInterval(timer)
      }
      const cancel = () => {
        finish()
        reject(new ClientError('oauthCancelled', 'AbortError'))
      }
      const listener = (event: MessageEvent) => {
        const data = event.data
        if (
          processing ||
          event.origin !== window.location.origin ||
          event.source !== popup ||
          data?.type !== 'ren2hub:oauth-bind-callback' ||
          data.provider !== provider ||
          data.state !== flow.flow_token
        )
          return
        processing = true
        void api
          .get<T>(`/api/oauth/${encodeURIComponent(provider)}`, {
            state: flow.flow_token,
            code: data.code,
            error: data.error,
            error_description: data.error_description,
          })
          .then((result) => {
            finish()
            popup.close()
            resolve(result)
          })
          .catch((error) => {
            finish()
            popup.close()
            // The callback rejects a browser without the cookie set with the
            // state, e.g. an expired flow or one started elsewhere.
            reject(
              error instanceof ApiError &&
                error.code === 'OAUTH_FLOW_BROWSER_MISMATCH'
                ? new ClientError('oauthBrowserMismatch')
                : error
            )
          })
      }
      const timer = window.setInterval(() => {
        if (popup.closed || Date.now() >= deadline) {
          finish()
          popup.close()
          reject(new ClientError('oauthCancelled', 'AbortError'))
        }
      }, 500)
      window.addEventListener('message', listener)
      window.addEventListener('beforeunload', cancel)
      signal?.addEventListener('abort', cancel, { once: true })
      popup.location.replace(safeURL)
    })
  } catch (error) {
    popup.close()
    throw error
  }
}
