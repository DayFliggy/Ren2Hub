import { ref } from 'vue'

/**
 * Why the last navigation failed, or null after a successful navigation.
 * `rate-limited` means a guard request was answered with HTTP 429. Keep the
 * failed route's href because an aborted navigation leaves the address bar
 * on the previous page, which is not the page the user needs to retry.
 */
export interface NavigationFailure {
  kind: 'rate-limited' | 'failed'
  href: string
}

export const navigationError = ref<NavigationFailure | null>(null)
