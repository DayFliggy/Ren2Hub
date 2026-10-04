import type { RelayTaskLogItem } from '@/types/console'

/**
 * How the result column presents a task (upstream resolveTaskPreviewMode).
 * Only successful, retained tasks request artifacts; /api/task/self never
 * carries admin_info, so plugin tasks are recognised by elimination.
 */
export type TaskPreviewMode =
  'artifacts' | 'legacy-suno' | 'legacy-video' | 'discarded' | 'none'

export function taskPreviewMode(
  task: Pick<
    RelayTaskLogItem,
    'status' | 'platform' | 'result_discarded' | 'legacy_video_available'
  >
): TaskPreviewMode {
  if (task.status !== 'SUCCESS') return 'none'
  if (task.result_discarded) return 'discarded'
  if (task.platform === 'suno') return 'legacy-suno'
  if (task.legacy_video_available) return 'legacy-video'
  return 'artifacts'
}
