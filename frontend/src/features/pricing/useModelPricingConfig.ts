import { computed, onBeforeUnmount, ref } from 'vue'
import { api } from '@/api/client'
import {
  pricingConfigApi,
  type ModelPricingChange,
  type ModelPricingConfig,
  type ModelPricingIssue,
} from '@/api/pricingConfig'
import { initialPricingDraft, pricingChanges, type PricingDraft } from './draft'
import { isPricingConflict } from './errors'

// rejected: nothing was written. reloadFailed: the server holds the draft but
// the page still has the old versions, so only a reload may continue.
export type PricingSaveResult =
  | { status: 'saved' }
  | { status: 'aborted' }
  | { status: 'rejected'; cause: unknown; conflict: boolean }
  | { status: 'reloadFailed'; cause: unknown }

// One request at a time owns the page: a newer load or an unmount aborts the
// older one, including the reload that follows a save.
export function useModelPricingConfig(
  loadSettings: (force: boolean) => Promise<boolean>
) {
  const baseline = ref<ModelPricingConfig | null>(null),
    draft = ref<PricingDraft>({}),
    enabledModels = ref<string[]>([])
  const issues = ref<ModelPricingIssue[]>([])
  const loading = ref(false),
    saving = ref(false),
    ready = ref(false),
    reloadRequired = ref(false)
  let controller: AbortController | undefined
  const changes = computed(() =>
    baseline.value ? pricingChanges(baseline.value, draft.value) : []
  )

  function begin() {
    controller?.abort()
    controller = new AbortController()
    return controller
  }
  function apply(config: ModelPricingConfig) {
    baseline.value = config
    draft.value = initialPricingDraft(config)
    reloadRequired.value = false
  }
  // Resolves false when a newer request replaced this one.
  async function load(forceSettings = false): Promise<boolean> {
    const request = begin()
    loading.value = true
    ready.value = false
    issues.value = []
    try {
      const [config, names, settingsLoaded, pricingIssues] = await Promise.all([
        pricingConfigApi.load(request.signal),
        api.get<string[]>('/api/channel/models_enabled', undefined, {
          signal: request.signal,
        }),
        loadSettings(forceSettings),
        pricingConfigApi.issues(request.signal),
      ])
      if (request.signal.aborted) return false
      if (!settingsLoaded) throw new Error('loadError')
      apply(config)
      enabledModels.value = names
      issues.value = pricingIssues
      ready.value = true
      return true
    } catch (cause) {
      if (request.signal.aborted) return false
      throw cause
    } finally {
      if (controller === request) loading.value = false
    }
  }
  async function save(
    pending: ModelPricingChange[]
  ): Promise<PricingSaveResult> {
    saving.value = true
    try {
      try {
        await pricingConfigApi.save(pending)
      } catch (cause) {
        return { status: 'rejected', cause, conflict: isPricingConflict(cause) }
      }
      const request = begin()
      try {
        const [config, pricingIssues] = await Promise.all([
          pricingConfigApi.load(request.signal),
          pricingConfigApi.issues(request.signal),
        ])
        if (request.signal.aborted) return { status: 'aborted' }
        apply(config)
        issues.value = pricingIssues
        return { status: 'saved' }
      } catch (cause) {
        if (request.signal.aborted) return { status: 'aborted' }
        reloadRequired.value = true
        ready.value = false
        return { status: 'reloadFailed', cause }
      }
    } finally {
      saving.value = false
    }
  }
  onBeforeUnmount(() => controller?.abort())
  return {
    baseline,
    draft,
    enabledModels,
    issues,
    changes,
    loading,
    saving,
    ready,
    reloadRequired,
    load,
    save,
  }
}
