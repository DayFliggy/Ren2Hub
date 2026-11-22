/**
 * Errors the browser client generates itself. They carry a stable code and are
 * translated where they are shown; server messages pass through unchanged.
 * Cancellations keep the browser's error name (AbortError, NotAllowedError) so
 * name-based cancellation checks still recognise them.
 */
export type ClientErrorCode =
  | 'oauthPopupBlocked'
  | 'oauthProviderUnavailable'
  | 'oauthInvalidAuthorizationUrl'
  | 'oauthCancelled'
  | 'oauthBrowserMismatch'
  | 'passkeyCancelled'
  | 'pluginLoginInvalidResponse'
  | 'pluginLoginInvalidFlow'
  | 'pluginLoginInvalidChannel'
  | 'checkoutInvalidUrl'
  | 'checkoutInvalidForm'

export class ClientError extends Error {
  constructor(
    public readonly code: ClientErrorCode,
    name = 'ClientError'
  ) {
    super(code)
    this.name = name
  }
}

export function errorMessage(
  error: unknown,
  t: (key: string) => string
): string {
  if (error instanceof ClientError)
    return t(`common.clientErrors.${error.code}`)
  return error instanceof Error ? error.message : String(error)
}
