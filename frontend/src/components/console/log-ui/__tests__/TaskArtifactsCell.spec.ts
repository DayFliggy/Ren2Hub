import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { getTaskArtifacts } from '@/api/taskArtifacts'
import TaskArtifactsCell from '@/components/console/log-ui/TaskArtifactsCell.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { RelayTaskLogItem } from '@/types/console'

vi.mock('@/api/taskArtifacts', () => ({ getTaskArtifacts: vi.fn() }))

const fetchArtifacts = vi.mocked(getTaskArtifacts)
const contentUrl = (key: string) =>
  `https://gateway.example/v1/tasks/task-public/artifacts/${key}/content?access=${'A'.repeat(43)}`
let wrapper: VueWrapper | undefined

function task(overrides: Partial<RelayTaskLogItem> = {}): RelayTaskLogItem {
  return {
    id: 1,
    task_id: 'task-public',
    platform: 'alibaba',
    action: 'text_to_image',
    status: 'SUCCESS',
    progress: '100%',
    fail_reason: '',
    quota: '25000',
    submit_time: 1,
    finish_time: 2,
    legacy_video_available: false,
    result_discarded: false,
    ...overrides,
  }
}

function render(row: RelayTaskLogItem): VueWrapper {
  wrapper = mount(TaskArtifactsCell, {
    props: { task: row },
    attachTo: document.body,
    global: { plugins: [i18n] },
  })
  return wrapper
}

function dialog(): HTMLElement {
  const element = document.body.querySelector<HTMLElement>('[role="dialog"]')
  if (!element) throw new Error('dialog is not open')
  return element
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => vi.clearAllMocks())
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

describe('TaskArtifactsCell', () => {
  it('shows a hint instead of the trigger for a discarded result and sends no request', () => {
    const cell = render(task({ result_discarded: true }))

    expect(cell.text()).toContain('Result not retained')
    expect(cell.find('[data-task-artifacts-trigger]').exists()).toBe(false)
    expect(fetchArtifacts).not.toHaveBeenCalled()
  })

  it('offers nothing for unfinished tasks', () => {
    const cell = render(task({ status: 'IN_PROGRESS' }))

    expect(cell.text()).toBe('-')
    expect(fetchArtifacts).not.toHaveBeenCalled()
  })

  it('requests artifacts only when opened and renders their signed URLs', async () => {
    fetchArtifacts.mockResolvedValue({
      task_id: 'task-public',
      artifacts: [
        {
          key: 'video-1',
          type: 'video',
          mime_type: 'video/mp4',
          content_url: contentUrl('video-1'),
        },
      ],
    })
    const cell = render(task())
    expect(fetchArtifacts).not.toHaveBeenCalled()

    await cell.get('[data-task-artifacts-trigger]').trigger('click')
    await flushPromises()

    expect(fetchArtifacts).toHaveBeenCalledWith(
      'task-public',
      expect.any(AbortSignal)
    )
    expect(dialog().querySelector('video')?.getAttribute('src')).toBe(
      contentUrl('video-1')
    )
    const download = dialog().querySelector('a[download]')
    expect(download?.getAttribute('href')).toBe(contentUrl('video-1'))
    expect(download?.getAttribute('aria-label')).toBe('Download video-1')
  })

  it('shows the empty state when the gateway reports artifact_not_found', async () => {
    fetchArtifacts.mockResolvedValue(null)
    const cell = render(task())

    await cell.get('[data-task-artifacts-trigger]').trigger('click')
    await flushPromises()

    expect(dialog().textContent).toContain('No artifacts')
    expect(dialog().querySelector('[role="alert"]')).toBeNull()
  })

  it('surfaces load failures with a retry', async () => {
    fetchArtifacts
      .mockRejectedValueOnce(new Error('Artifact preview plugin failed'))
      .mockResolvedValueOnce({ task_id: 'task-public', artifacts: [] })
    const cell = render(task())

    await cell.get('[data-task-artifacts-trigger]').trigger('click')
    await flushPromises()
    const alert = dialog().querySelector('[role="alert"]')
    expect(alert?.textContent).toContain('Artifact preview plugin failed')

    alert?.querySelector('button')?.click()
    await flushPromises()
    expect(fetchArtifacts).toHaveBeenCalledTimes(2)
    expect(dialog().textContent).toContain('No artifacts')
  })

  it('plays a legacy video through the legacy content URL', async () => {
    fetchArtifacts.mockResolvedValue({
      task_id: 'task-public',
      artifacts: [],
      legacy_content_url: contentUrl('video'),
    })
    const cell = render(
      task({ platform: 'kling', legacy_video_available: true })
    )

    const trigger = cell.get('[data-task-artifacts-trigger]')
    expect(trigger.text()).toBe('Click to preview video')
    await trigger.trigger('click')
    await flushPromises()

    expect(dialog().querySelector('video')?.getAttribute('src')).toBe(
      contentUrl('video')
    )
  })

  it('lists legacy Suno clips from the artifacts response', async () => {
    fetchArtifacts.mockResolvedValue({
      task_id: 'task-public',
      artifacts: [],
      legacy_audio_clips: [
        { audio_url: 'https://cdn.example.com/a.mp3', title: 'First' },
        { audio_url: 'https://cdn.example.com/b.mp3' },
      ],
    })
    const cell = render(task({ platform: 'suno' }))

    const trigger = cell.get('[data-task-artifacts-trigger]')
    expect(trigger.text()).toBe('Click to preview audio')
    await trigger.trigger('click')
    await flushPromises()

    const clips = dialog().querySelectorAll('[data-task-audio-clip]')
    expect(clips).toHaveLength(2)
    expect(clips[0].querySelector('audio')?.getAttribute('src')).toBe(
      'https://cdn.example.com/a.mp3'
    )
    expect(clips[1].textContent).toContain('Untitled')
  })
})
