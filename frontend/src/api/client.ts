import { createApiClient, setUnauthorizedHandler } from './createClient'
import { mockTransport } from './mock/transport'

export const api = createApiClient(mockTransport)
export { setUnauthorizedHandler }
