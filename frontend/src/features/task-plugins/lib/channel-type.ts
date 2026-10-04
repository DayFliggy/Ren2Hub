// Stands in for upstream `t(getChannelTypeLabel(type))` using Ren2Hub's
// channel catalog. Brand names stay untranslated; the generic Task Plugin
// type uses the localized label.
import { ADMIN_CHANNEL_TYPE_META } from '@/constants/adminChannels'

const TASK_PLUGIN_CHANNEL_TYPE = 61

export function channelTypeLabel(
  type: number,
  taskPluginLabel: string
): string {
  if (type === TASK_PLUGIN_CHANNEL_TYPE) return taskPluginLabel
  return ADMIN_CHANNEL_TYPE_META[type]?.label ?? `#${type}`
}
