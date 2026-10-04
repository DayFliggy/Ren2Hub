// Mirrors upstream lib/host-protocols.ts and `hostProtocols` in
// pkg/jsplugin/routing.go. The table is frozen under apiVersion 1, so it is
// mapped client-side; colon params are written in `{brace}` form.

export type HostProtocolEndpoint = {
  method: string
  path: string
  /** The create call whose request forms `supports` gates. */
  modeBearing?: boolean
}

export const HOST_PROTOCOL_ENDPOINTS: Record<string, HostProtocolEndpoint[]> = {
  openai_responses: [
    { method: 'POST', path: '/v1/responses', modeBearing: true },
    { method: 'GET', path: '/v1/responses/{response_id}' },
  ],
  openai_video: [
    { method: 'POST', path: '/v1/videos', modeBearing: true },
    { method: 'GET', path: '/v1/videos/{task_id}' },
    { method: 'GET', path: '/v1/videos/{task_id}/content' },
  ],
  // Synchronous: the create call returns the rendered image response.
  openai_image: [
    { method: 'POST', path: '/v1/images/generations' },
    { method: 'POST', path: '/v1/images/edits' },
  ],
}
