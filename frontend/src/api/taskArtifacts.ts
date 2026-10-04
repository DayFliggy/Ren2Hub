import type { TaskArtifactsResult } from '@/types/console'

import { api } from './client'
import { parseTaskArtifactsResult } from './liveContracts'
import { ApiError } from './types'

function isArtifactNotFound(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    error.status === 404 &&
    error.code === 'artifact_not_found'
  )
}

/**
 * Reads a task's result artifacts. Resolves null when the gateway reports
 * artifact_not_found (result discarded or no longer retrievable); every other
 * failure, including an unexpected 404, is rethrown for the caller to show.
 */
export async function getTaskArtifacts(
  taskId: string,
  signal?: AbortSignal
): Promise<TaskArtifactsResult | null> {
  try {
    const value = await api.get<unknown>(
      `/api/task/${encodeURIComponent(taskId)}/artifacts`,
      undefined,
      { signal }
    )
    return parseTaskArtifactsResult(value)
  } catch (error) {
    if (isArtifactNotFound(error)) return null
    throw error
  }
}
