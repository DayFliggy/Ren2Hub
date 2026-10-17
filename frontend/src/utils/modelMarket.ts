export function isAudioEndpoint(endpoint: string): boolean {
  return endpoint === 'realtime' || endpoint.includes('audio')
}
