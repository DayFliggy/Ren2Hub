import type { InjectionKey, Ref } from 'vue'

/** Pricing header state and actions owned by ModelPricingView. */
export interface PricingBarState {
  dirty: boolean
  loading: boolean
  saving: boolean
  settingsSaving: boolean
  ready: boolean
  tab: string
  /** Models whose saved price the next save changes. */
  changeCount: number
  reload: () => void
  beginFetch: () => void
  /** Opens the change review; saving waits for its confirmation. */
  save: () => void
}

/**
 * The page hosting an embedded pricing view renders its header bar. The view
 * registers its live state once on setup and clears it on unmount.
 */
export const pricingBarHostKey: InjectionKey<
  (state: Readonly<Ref<PricingBarState>> | null) => void
> = Symbol('pricingBarHost')
