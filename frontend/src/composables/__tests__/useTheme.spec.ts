import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick, watch } from 'vue'
import { useTheme } from '../useTheme'

const theme = useTheme()

function installTransitions() {
  const transitions: {
    run: () => Promise<void>
    skip: ReturnType<typeof vi.fn>
    finish: () => void
  }[] = []
  const start = vi.fn((update: () => Promise<void>) => {
    let complete!: () => void
    let fail!: (reason: unknown) => void
    let finish!: () => void
    const updateCallbackDone = new Promise<void>((resolve, reject) => {
      complete = resolve
      fail = reject
    })
    const finished = new Promise<void>((resolve) => {
      finish = resolve
    })
    const skip = vi.fn(finish)
    const transition = {
      skip,
      finish,
      async run() {
        try {
          await update()
          complete()
        } catch (error) {
          fail(error)
        }
      },
    }
    transitions.push(transition)
    return {
      ready: Promise.resolve(),
      updateCallbackDone,
      finished,
      skipTransition: skip,
    }
  })
  Object.defineProperty(document, 'startViewTransition', {
    configurable: true,
    value: start,
  })
  return { start, transitions }
}

afterEach(async () => {
  await theme.setThemePreference('auto')
  Reflect.deleteProperty(document, 'startViewTransition')
  vi.restoreAllMocks()
})

describe('shared theme transitions', () => {
  it('applies programmatic changes immediately without an animation', async () => {
    const { start } = installTransitions()
    document.head.insertAdjacentHTML(
      'beforeend',
      '<meta name="theme-color" content="">'
    )
    await theme.setThemePreference('dark')
    expect(theme.preference.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.documentElement.style.colorScheme).toBe('dark')
    expect(
      document
        .querySelector('meta[name="theme-color"]')
        ?.getAttribute('content')
    ).toBe('#1C1F26')
    expect(localStorage.getItem('ren2hub_theme_mode')).toBe('dark')
    expect(start).not.toHaveBeenCalled()
  })

  it('waits for post-flush consumers before completing the new snapshot', async () => {
    await theme.setThemePreference('light')
    const { transitions } = installTransitions()
    let consumerTheme = ''
    const stop = watch(
      theme.resolvedTheme,
      () => {
        consumerTheme = document.documentElement.dataset.theme!
      },
      { flush: 'post' }
    )
    const done = theme.setThemePreference('dark', { animate: true })
    expect(document.documentElement.dataset.theme).toBe('light')
    await transitions[0]!.run()
    expect(consumerTheme).toBe('dark')
    transitions[0]!.finish()
    await done
    expect(document.documentElement.hasAttribute('data-theme-transition')).toBe(
      false
    )
    stop()
  })

  it('does not let an older delayed update overwrite the latest choice', async () => {
    await theme.setThemePreference('light')
    const { transitions } = installTransitions()
    const old = theme.setThemePreference('dark', { animate: true })
    await theme.setThemePreference('light', { animate: true })
    expect(transitions[0]!.skip).toHaveBeenCalledOnce()
    await transitions[0]!.run()
    await old
    expect(theme.preference.value).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.hasAttribute('data-theme-transition')).toBe(
      false
    )
  })

  it('changes the preference without animating when the resolved theme is unchanged', async () => {
    await theme.setThemePreference('light')
    const { start } = installTransitions()
    await theme.setThemePreference('auto', { animate: true })
    expect(theme.preference.value).toBe('auto')
    expect(theme.resolvedTheme.value).toBe('light')
    expect(start).not.toHaveBeenCalled()
  })

  it('waits for visible themed artwork to decode before the new snapshot', async () => {
    await theme.setThemePreference('light')
    const image = document.createElement('img')
    image.dataset.themeAsset = ''
    document.body.append(image)
    vi.spyOn(image, 'getClientRects').mockReturnValue([
      {},
    ] as unknown as DOMRectList)
    let decoded!: () => void
    image.decode = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          decoded = resolve
        })
    )
    const { transitions } = installTransitions()
    const done = theme.setThemePreference('dark', { animate: true })
    const rendering = transitions[0]!.run()
    await nextTick()
    expect(image.decode).toHaveBeenCalledOnce()
    expect(document.documentElement.hasAttribute('data-theme-transition')).toBe(
      true
    )
    decoded()
    await rendering
    transitions[0]!.finish()
    await done
    image.remove()
  })

  it.each([false, true])(
    'surfaces image decoding failures unless the snapshot was superseded (%s)',
    async (superseded) => {
      await theme.setThemePreference('light')
      const image = document.createElement('img')
      image.dataset.themeAsset = ''
      document.body.append(image)
      vi.spyOn(image, 'getClientRects').mockReturnValue([
        {},
      ] as unknown as DOMRectList)
      let rejectDecode!: (error: Error) => void
      image.decode = vi.fn(
        () =>
          new Promise<void>((_resolve, reject) => {
            rejectDecode = reject
          })
      )
      const { transitions } = installTransitions()
      try {
        const outcome = theme
          .setThemePreference('dark', { animate: true })
          .then(
            () => null,
            (error: unknown) => error
          )
        const rendering = transitions[0]!.run()
        await nextTick()
        expect(image.decode).toHaveBeenCalledOnce()
        if (superseded) await theme.setThemePreference('light')
        const error = new DOMException(
          'The source image cannot be decoded.',
          'EncodingError'
        )
        rejectDecode(error)
        await rendering
        transitions[0]!.finish()
        expect(await outcome).toBe(superseded ? null : error)
        expect(theme.resolvedTheme.value).toBe(superseded ? 'light' : 'dark')
        expect(
          document.documentElement.hasAttribute('data-theme-transition')
        ).toBe(false)
      } finally {
        image.remove()
      }
    }
  )

  it('cleans the animation marker if the browser rejects creation', async () => {
    await theme.setThemePreference('light')
    Object.defineProperty(document, 'startViewTransition', {
      configurable: true,
      value: () => {
        throw new Error('transition creation failed')
      },
    })
    await expect(
      theme.setThemePreference('dark', { animate: true })
    ).rejects.toThrow('transition creation failed')
    expect(document.documentElement.hasAttribute('data-theme-transition')).toBe(
      false
    )
  })

  it('respects reduced motion and works without the transition API', async () => {
    await theme.setThemePreference('light')
    const { start } = installTransitions()
    const original = window.matchMedia
    vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
      ...original(query),
      matches: query === '(prefers-reduced-motion: reduce)',
    }))
    await theme.setThemePreference('dark', { animate: true })
    expect(start).not.toHaveBeenCalled()
    Reflect.deleteProperty(document, 'startViewTransition')
    await theme.setThemePreference('light', { animate: true })
    await nextTick()
    expect(document.documentElement.dataset.theme).toBe('light')
  })
})
