import type { InjectionKey, Ref } from 'vue'

/** Pricing header state and actions owned by ModelPricingView. */
export interface PricingBarState {
  dirty: boolean
  loading: boolean
  saving: boolean
  settingsSaving: boolean
  ready: boolean
  tab: string
  reload: () => void
  beginFetch: () => void
  save: () => Promise<void>
}

/**
 * The page hosting an embedded pricing view renders its header bar. The view
 * registers its live state once on setup and clears it on unmount.
 */
export const pricingBarHostKey: InjectionKey<
  (state: Readonly<Ref<PricingBarState>> | null) => void
> = Symbol('pricingBarHost')
