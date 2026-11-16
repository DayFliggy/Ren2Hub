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

/**
 * True from the first guard of a navigation inside the running app until the
 * latest navigation is confirmed, aborted or fails. A navigation that a newer
 * one replaced does not clear it; the first page load never sets it.
 */
export const navigationPending = ref(false)
