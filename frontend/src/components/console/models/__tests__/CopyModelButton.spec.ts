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
import CopyModelButton from '@/components/console/models/CopyModelButton.vue'
import { useToast } from '@/composables/useToast'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const originalClipboard = Object.getOwnPropertyDescriptor(
  navigator,
  'clipboard'
)
const wrappers: VueWrapper[] = []
const toast = useToast()
function render(writeText: (text: string) => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText },
  })
  const wrapper = mount(CopyModelButton, {
    props: { name: 'provider/model-preview' },
    global: { plugins: [i18n] },
  })
  wrappers.push(wrapper)
  return wrapper
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
  toast.toasts.splice(0)
})
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  toast.toasts.splice(0)
  vi.clearAllTimers()
  vi.useRealTimers()
  if (originalClipboard)
    Object.defineProperty(navigator, 'clipboard', originalClipboard)
  else Reflect.deleteProperty(navigator, 'clipboard')
})

describe('model ID clipboard feedback', () => {
  it('reports success only after the clipboard actually accepts the exact model ID', async () => {
    let accept!: () => void
    const writeText = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          accept = resolve
        })
    )
    const wrapper = render(writeText)
    await wrapper.get('button').trigger('click')
    expect(writeText).toHaveBeenCalledExactlyOnceWith('provider/model-preview')
    expect(toast.toasts).toHaveLength(0)
    accept()
    await flushPromises()
    expect(toast.toasts).toMatchObject([
      { type: 'success', message: '已复制 provider/model-preview' },
    ])
  })

  it('reports clipboard permission rejection without a false success message', async () => {
    const writeText = vi
      .fn()
      .mockRejectedValue(
        new DOMException('Permission denied', 'NotAllowedError')
      )
    const wrapper = render(writeText)
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledExactlyOnceWith('provider/model-preview')
    expect(toast.toasts).toMatchObject([
      { type: 'error', message: '复制失败，请手动复制模型 ID' },
    ])
    expect(toast.toasts.some((item) => item.type === 'success')).toBe(false)
  })
})
