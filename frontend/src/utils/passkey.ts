import { ClientError } from '@/api/clientError'

export function decodeBase64URL(value: string): ArrayBuffer {
  const normalized = value.replace(/-/g, '+').replace(/_/g, '/')
  return Uint8Array.from(
    atob(normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=')),
    (c) => c.charCodeAt(0)
  ).buffer
}

function encodeBase64URL(value: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(value)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')
}

export interface AssertionOptions {
  publicKey: Omit<
    PublicKeyCredentialRequestOptions,
    'challenge' | 'allowCredentials'
  > & {
    challenge: string
    allowCredentials?: Array<
      Omit<PublicKeyCredentialDescriptor, 'id'> & { id: string }
    >
  }
}

export async function assertPasskey(
  options: AssertionOptions
): Promise<Record<string, unknown>> {
  const wire = options.publicKey
  const credential = (await navigator.credentials.get({
    publicKey: {
      ...wire,
      challenge: decodeBase64URL(wire.challenge),
      allowCredentials: wire.allowCredentials?.map((item) => ({
        ...item,
        id: decodeBase64URL(item.id),
      })),
    },
  })) as PublicKeyCredential | null
  if (!credential) throw new ClientError('passkeyCancelled', 'NotAllowedError')
  const response = credential.response as AuthenticatorAssertionResponse
  return {
    id: credential.id,
    rawId: encodeBase64URL(credential.rawId),
    type: credential.type,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      clientDataJSON: encodeBase64URL(response.clientDataJSON),
      authenticatorData: encodeBase64URL(response.authenticatorData),
      signature: encodeBase64URL(response.signature),
      userHandle: response.userHandle
        ? encodeBase64URL(response.userHandle)
        : null,
    },
  }
}
