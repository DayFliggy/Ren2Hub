import { createPinia } from 'pinia'
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

import { clearAuthBundle } from '@/api/authSession'
import { api } from '@/api/console'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import LogsView from '@/views/console/LogsView.vue'

const toast = vi.hoisted(() => ({
  push: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
  warning: vi.fn(),
  info: vi.fn(),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
// Small files let a few rows produce several volumes.
vi.mock('@/utils/exportVolumes', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/utils/exportVolumes')>()
  return {
    ...actual,
    createExportVolumes: (
      ...[format, save]: Parameters<typeof actual.createExportVolumes>
    ) => actual.createExportVolumes(format, save, 600),
  }
})

let wrapper: VueWrapper | null = null
let source: ReadableStreamDefaultController<Uint8Array>
const cancelStream = vi.fn()
const downloads: string[] = []
const encoder = new TextEncoder()

function row(id: number) {
  return JSON.stringify({
    id,
    type: 2,
    token_name: 'fixture',
    model_name: 'model',
    channel_name: 'channel',
    quota: 1,
    prompt_tokens: 1,
    completion_tokens: 2,
    use_time: 1,
    is_stream: true,
    content: 'row',
    created_at: 1700000000,
  })
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(() => {
  downloads.length = 0
  cancelStream.mockReset()
  vi.spyOn(api, 'get').mockImplementation((path: string) => {
    if (path === '/api/log/self')
      return Promise.resolve({ page: 1, page_size: 10, total: 0, items: [] })
    return Promise.reject(new Error(`Unexpected API request: ${path}`))
  })
  vi.spyOn(api, 'getStream').mockImplementation(async () => {
    return new ReadableStream({
      start: (controller) => void (source = controller),
      cancel: cancelStream,
    })
  })
  URL.createObjectURL = vi.fn(() => 'blob:export')
  URL.revokeObjectURL = vi.fn()
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (
    this: HTMLAnchorElement
  ) {
    downloads.push(this.download)
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
  Object.values(toast).forEach((spy) => spy.mockReset())
})

async function startExport() {
  wrapper = mount(LogsView, {
    attachTo: document.body,
    global: { plugins: [createPinia(), i18n], stubs: { LogsNavTabs: true } },
  })
  await flushPromises()
  const open = wrapper
    .findAll('button')
    .find((button) => button.text() === '导出')!
  await open.trigger('click')
  await flushPromises()
  const dialog = () => document.body.querySelector('[role="dialog"]')!
  const button = (name: string) =>
    [...dialog().querySelectorAll('button')].find(
      (item) => item.textContent?.trim() === name
    )
  button('确认')!.click()
  await flushPromises()
  return { dialog, button }
}

async function nextFrame() {
  await new Promise((resolve) => requestAnimationFrame(resolve))
  await flushPromises()
}

describe('LogsView export progress', () => {
  it('shows the running count and saved files, and turns confirm into cancel', async () => {
    const { dialog, button } = await startExport()
    expect(button('取消导出')).toBeDefined()
    expect(button('确认')).toBeUndefined()

    source.enqueue(encoder.encode(row(1) + '\n'))
    await flushPromises()
    await nextFrame()
    expect(dialog().textContent).toContain('已导出 1 条')
    expect(dialog().textContent).not.toContain('已保存')

    source.enqueue(encoder.encode([2, 3, 4].map(row).join('\n') + '\n'))
    await flushPromises()
    await nextFrame()
    expect(downloads.length).toBeGreaterThan(0)
    expect(dialog().textContent).toContain(
      `已导出 4 条，已保存 ${downloads.length} 个文件`
    )
  })

  it('cancels on request, keeps the saved files and says they are partial', async () => {
    const { dialog, button } = await startExport()
    source.enqueue(encoder.encode([1, 2, 3, 4].map(row).join('\n') + '\n'))
    await flushPromises()
    const saved = [...downloads]
    expect(saved.length).toBeGreaterThan(0)
    expect(saved[0]).toMatch(/^ren2hub-logs-all-\d{8}-part1\.csv$/)

    button('取消导出')!.click()
    await flushPromises()
    expect(cancelStream).toHaveBeenCalledOnce()
    expect(downloads).toEqual(saved)
    expect(toast.warning).toHaveBeenCalledWith(
      `导出已取消，已下载的 ${saved.length} 个文件只包含部分记录。`
    )
    expect(toast.error).not.toHaveBeenCalled()
    // The dialog stays open, ready for another export.
    expect(button('确认')).toBeDefined()
    expect(dialog().textContent).not.toContain('已导出 4 条')
  })

  it('warns about multiple downloads once a second file starts', async () => {
    const { dialog } = await startExport()
    const notice =
      '导出较大，将分多个文件下载。浏览器若询问是否允许下载多个文件，请选择允许。'
    source.enqueue(encoder.encode(row(1) + '\n'))
    await flushPromises()
    await nextFrame()
    expect(dialog().textContent).not.toContain(notice)
    source.enqueue(encoder.encode([2, 3, 4].map(row).join('\n') + '\n'))
    await flushPromises()
    await nextFrame()
    expect(downloads.length).toBeGreaterThan(0)
    expect(dialog().textContent).toContain(notice)
  })

  it('tells how many files a multi-file export should have produced', async () => {
    await startExport()
    source.enqueue(
      encoder.encode(
        [1, 2, 3, 4].map(row).join('\n') +
          '\n' +
          JSON.stringify({ success: true, data: { count: 4 } })
      )
    )
    source.close()
    await flushPromises()
    const files = downloads.length
    expect(files).toBeGreaterThan(1)
    // Long enough to compare against the download list, unlike the default.
    expect(toast.push).toHaveBeenCalledWith(
      'success',
      `已导出 4 条，分 ${files} 个文件；若下载列表中少于 ${files} 个文件，请允许本站下载多个文件后重新导出。`,
      10_000
    )
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('keeps the single-file message unchanged', async () => {
    await startExport()
    source.enqueue(
      encoder.encode(
        row(1) + '\n' + JSON.stringify({ success: true, data: { count: 1 } })
      )
    )
    source.close()
    await flushPromises()
    expect(downloads).toHaveLength(1)
    expect(toast.success).toHaveBeenCalledWith('已导出 1 条记录')
  })

  it('stops when the sign-in changes and reports the saved files as partial', async () => {
    const { button } = await startExport()
    source.enqueue(encoder.encode([1, 2, 3, 4].map(row).join('\n') + '\n'))
    await flushPromises()
    const saved = [...downloads]
    expect(saved.length).toBeGreaterThan(0)
    clearAuthBundle()
    source.enqueue(encoder.encode([5, 6, 7, 8].map(row).join('\n') + '\n'))
    await flushPromises()
    expect(downloads).toEqual(saved)
    expect(cancelStream).toHaveBeenCalledOnce()
    expect(toast.error).toHaveBeenCalledWith(
      `登录状态已变化，导出已停止。 已下载的 ${saved.length} 个文件只包含部分记录，请重新导出。`
    )
    expect(button('确认')).toBeDefined()
  })

  it('reports a sign-in change before any file was saved', async () => {
    await startExport()
    source.enqueue(encoder.encode(row(1) + '\n'))
    await flushPromises()
    clearAuthBundle()
    source.enqueue(
      encoder.encode(JSON.stringify({ success: true, data: { count: 1 } }))
    )
    source.close()
    await flushPromises()
    expect(downloads).toEqual([])
    expect(toast.error).toHaveBeenCalledWith('登录状态已变化，导出已停止。')
    expect(toast.success).not.toHaveBeenCalled()
  })
})
