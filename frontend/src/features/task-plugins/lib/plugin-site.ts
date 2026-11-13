import { useStorage } from '@vueuse/core'
export const pluginSite = useStorage<'ai' | 'pro'>('ren2hub-plugin-site', 'ai')
export function resolvePluginSiteUrl(input: string | URL): string {
  const url = new URL(input)
  if (url.protocol !== 'https:' || url.username || url.password)
    throw new Error('Invalid plugin site URL')
  if (pluginSite.value === 'pro' && url.origin === 'https://www.newapi.ai')
    url.hostname = 'www.newapi.pro'
  return url.href
}
/** Site choice only changes built-in download addresses; credentials never leave the console. */
export async function fetchFromPluginSite(
  input: string | URL,
  init: RequestInit = {}
): Promise<Response> {
  return fetch(resolvePluginSiteUrl(input), {
    ...init,
    credentials: 'omit',
    redirect: 'error',
    referrerPolicy: 'no-referrer',
  })
}
