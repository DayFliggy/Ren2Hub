import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../client', () => ({ api: mocks }))

import { getTaskArtifacts } from '../taskArtifacts'
import { ApiError } from '../types'

const contentUrl =
  'https://gateway.example/v1/tasks/task%2F1/artifacts/image-1/content?access=' +
  'A'.repeat(43)

beforeEach(() => vi.clearAllMocks())

describe('getTaskArtifacts', () => {
  it('requests the encoded task path and parses the projection', async () => {
    mocks.get.mockResolvedValue({
      task_id: 'task/1',
      artifacts: [{ key: 'image-1', type: 'image', content_url: contentUrl }],
    })
    const signal = new AbortController().signal

    const result = await getTaskArtifacts('task/1', signal)

    expect(mocks.get).toHaveBeenCalledWith(
      '/api/task/task%2F1/artifacts',
      undefined,
      { signal }
    )
    expect(result?.artifacts).toEqual([
      { key: 'image-1', type: 'image', content_url: contentUrl },
    ])
  })

  it('resolves null for artifact_not_found', async () => {
    mocks.get.mockRejectedValue(
      new ApiError('Task or artifact not found', {
        status: 404,
        code: 'artifact_not_found',
      })
    )

    await expect(getTaskArtifacts('task_1')).resolves.toBeNull()
  })

  it('rethrows other failures, including a 404 without the artifact code', async () => {
    const unavailable = new ApiError('Artifact preview plugin is unavailable', {
      status: 503,
      code: 'artifact_plugin_unavailable',
    })
    mocks.get.mockRejectedValueOnce(unavailable)
    await expect(getTaskArtifacts('task_1')).rejects.toBe(unavailable)

    const missingRoute = new ApiError('Not Found', { status: 404 })
    mocks.get.mockRejectedValueOnce(missingRoute)
    await expect(getTaskArtifacts('task_1')).rejects.toBe(missingRoute)

    mocks.get.mockResolvedValueOnce({ task_id: 'task_1', artifacts: 'none' })
    await expect(getTaskArtifacts('task_1')).rejects.toMatchObject({
      code: 'INVALID_RESPONSE',
    })
  })
})
