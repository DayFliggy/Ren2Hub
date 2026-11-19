// Upstream web/src/features/task-plugins/lib/plugin-scope.ts.
import type {
  RequestPluginInstance,
  RequestPluginScope,
  TaskPluginOption,
} from '../types'

/** The scope a plugin declares now, as an instance confirms it; null for none. */
export function declaredPluginScope(
  plugin: Pick<TaskPluginOption, 'scope'> | undefined
): RequestPluginScope | null {
  const scope = plugin?.scope
  if (!scope) return null
  const models = scope.models ?? []
  const channelTypes = scope.channelTypes ?? []
  if (models.length === 0 && channelTypes.length === 0) return null
  return { models: [...models], channel_types: [...channelTypes] }
}

function sameItems<T>(a: readonly T[], b: readonly T[]): boolean {
  return a.length === b.length && a.every((item) => b.includes(item))
}

/** Whether two scopes hold the same models and channel types, in any order. */
export function samePluginScope(
  a: RequestPluginScope,
  b: RequestPluginScope
): boolean {
  return (
    sameItems(a.models, b.models) && sameItems(a.channel_types, b.channel_types)
  )
}

/**
 * Whether an instance that follows its plugin's scope confirmed an older
 * one: the plugin changed where it runs, and the change waits for review.
 * False for an instance placed by hand or a plugin that is gone.
 */
export function pluginScopeNeedsReview(
  instance: RequestPluginInstance,
  plugin: TaskPluginOption | undefined
): boolean {
  if (!instance.plugin_scope || !plugin) return false
  const declared = declaredPluginScope(plugin)
  return !declared || !samePluginScope(instance.plugin_scope, declared)
}
