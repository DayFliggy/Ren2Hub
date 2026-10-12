import { ref } from 'vue'

/**
 * Why the last navigation failed, or null after a successful navigation.
 * `rate-limited` means a guard request was answered with HTTP 429; the page
 * stays retryable instead of falling back to a signed-out state.
 */
export type NavigationFailure = 'rate-limited' | 'failed'

export const navigationError = ref<NavigationFailure | null>(null)
