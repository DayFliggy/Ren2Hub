import { computed, nextTick, watch } from 'vue'
import { useColorMode } from '@vueuse/core'

import { migrateLocalStorageKey } from '@/utils/legacyStorage'
import {
  isThemePreference,
  LEGACY_THEME_STORAGE_KEY,
  THEME_STORAGE_KEY,
  type ThemePreference,
} from '@/utils/themePreference'

export type { ThemePreference } from '@/utils/themePreference'
export type ResolvedTheme = Exclude<ThemePreference, 'auto'>
export { THEME_STORAGE_KEY } from '@/utils/themePreference'

// This module evaluates before main.ts runs its migrations (import hoisting),
// so the theme key migrates here — useColorMode below reads it immediately.
if (typeof window !== 'undefined') {
  migrateLocalStorageKey(LEGACY_THEME_STORAGE_KEY, THEME_STORAGE_KEY)
}

function clearInvalidStoredPreference() {
  if (typeof window === 'undefined') return

  try {
    const storedPreference = window.localStorage.getItem(THEME_STORAGE_KEY)
    if (storedPreference && !isThemePreference(storedPreference)) {
      window.localStorage.removeItem(THEME_STORAGE_KEY)
    }
  } catch {
    // VueUse falls back gracefully when storage is unavailable.
  }
}

clearInvalidStoredPreference()

let activeTransition: ViewTransition | null = null
let requestVersion = 0
let writingPreference = false

function applyDocumentTheme(theme: ResolvedTheme) {
  if (typeof document === 'undefined') return
  const root = document.documentElement
  root.dataset.theme = theme
  root.style.colorScheme = theme
  document
    .querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    ?.setAttribute('content', theme === 'dark' ? '#1C1F26' : '#F6F3EB')
}

const colorMode = useColorMode<ThemePreference>({
  selector: 'html',
  attribute: 'class',
  initialValue: 'auto',
  storageKey: THEME_STORAGE_KEY,
  modes: {
    auto: '',
    light: 'light',
    dark: 'dark',
  },
  onChanged(mode, applyClasses) {
    // VueUse suppresses incidental component color transitions while applying
    // classes. The explicit root snapshot owns the only theme animation.
    applyClasses(mode)
    applyDocumentTheme(mode === 'dark' ? 'dark' : 'light')
  },
})

const preference = computed<ThemePreference>(() =>
  isThemePreference(colorMode.store.value) ? colorMode.store.value : 'auto'
)

const resolvedTheme = computed<ResolvedTheme>(() =>
  colorMode.state.value === 'dark' ? 'dark' : 'light'
)

function cancelTransition() {
  activeTransition?.skipTransition()
  activeTransition = null
  if (typeof document !== 'undefined')
    delete document.documentElement.dataset.themeTransition
}

// A system/storage update supersedes an in-flight local choice. It must never
// replay an old snapshot or allow a delayed callback to overwrite the update.
watch(
  colorMode.store,
  () => {
    if (writingPreference) return
    requestVersion += 1
    cancelTransition()
  },
  { flush: 'sync' }
)
watch(
  colorMode.system,
  () => {
    if (preference.value !== 'auto') return
    requestVersion += 1
    cancelTransition()
  },
  { flush: 'sync' }
)

function writePreference(value: ThemePreference) {
  writingPreference = true
  try {
    colorMode.store.value = value
  } finally {
    writingPreference = false
  }
}

async function setThemePreference(
  value: ThemePreference,
  options: { animate?: boolean } = {}
): Promise<void> {
  if (!isThemePreference(value)) return
  const version = ++requestVersion
  cancelTransition()
  const target = value === 'auto' ? colorMode.system.value : value
  const animate =
    options.animate &&
    target !== resolvedTheme.value &&
    typeof document !== 'undefined' &&
    document.visibilityState !== 'hidden' &&
    typeof document.startViewTransition === 'function' &&
    !window.matchMedia('(prefers-reduced-motion: reduce)').matches

  if (!animate) {
    writePreference(value)
    await nextTick()
    return
  }

  document.documentElement.dataset.themeTransition = ''
  let transition: ViewTransition
  try {
    transition = document.startViewTransition(async () => {
      if (version !== requestVersion) return
      writePreference(value)
      // Post-flush consumers draw Canvas before the destination snapshot.
      await nextTick()
      // Only theme-dependent artwork participates; unrelated remote images
      // must not hold the theme change open. decode also covers a cold cache.
      try {
        await Promise.all(
          [
            ...document.querySelectorAll<HTMLImageElement>(
              'img[data-theme-asset]'
            ),
          ]
            .filter((image) => image.getClientRects().length > 0)
            .map((image) => image.decode())
        )
      } catch (error) {
        // A newer theme can replace an image while the discarded snapshot is
        // decoding it. Only obsolete work is cancelled; current failures surface.
        if (version === requestVersion) throw error
      }
    })
  } catch (error) {
    delete document.documentElement.dataset.themeTransition
    throw error
  }
  activeTransition = transition
  void transition.ready.catch((error: unknown) => {
    // Resize, a hidden document, or a newer choice can skip the animation.
    // DOM-update failures are still propagated via updateCallbackDone below.
    if (
      error instanceof DOMException &&
      (error.name === 'AbortError' || error.name === 'InvalidStateError')
    )
      return
    console.error('Theme transition could not start', error)
  })
  try {
    await Promise.all([transition.updateCallbackDone, transition.finished])
  } finally {
    if (activeTransition === transition) {
      activeTransition = null
      delete document.documentElement.dataset.themeTransition
    }
  }
}

/**
 * Shared application theme state. `preference` keeps the user's three-state
 * selection, while `resolvedTheme` is always the light or dark value applied
 * to the document root by VueUse.
 */
export function useTheme() {
  return {
    preference,
    resolvedTheme,
    setThemePreference,
  }
}
