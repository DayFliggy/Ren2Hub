import { inject, onScopeDispose, provide, type InjectionKey } from 'vue'
import { useUnsavedChanges } from '@/composables/useUnsavedChanges'

interface SettingsDraft {
  dirty: () => boolean
  busy: () => boolean
}
const draftsKey: InjectionKey<Set<SettingsDraft>> = Symbol('settings-drafts')

/** One leave decision covers the section form and its dedicated integrations. */
export function provideSettingsDraftGuard(
  section: SettingsDraft,
  message: () => string
) {
  const drafts = new Set<SettingsDraft>([section])
  provide(draftsKey, drafts)
  useUnsavedChanges({
    dirty: () => [...drafts].some((draft) => draft.dirty()),
    busy: () => [...drafts].some((draft) => draft.busy()),
    message,
  })
}

export function registerSettingsDraft(draft: SettingsDraft) {
  const drafts = inject(draftsKey, undefined)
  drafts?.add(draft)
  onScopeDispose(() => drafts?.delete(draft))
}
