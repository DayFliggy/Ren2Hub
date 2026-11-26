<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'

import {
  closeModalDialog,
  isTopmostModalDialog,
  lockBodyScroll,
  openModalDialog,
  unlockBodyScroll,
} from '@/utils/modalStack'
import { containsWithFloatingPanels, tabbableElements } from '@/utils/focusable'

const props = withDefaults(
  defineProps<{
    open: boolean
    title?: string
    subtitle?: string
    ariaLabel?: string
    /** Describes the dialog when its text is in the default slot. */
    ariaDescribedby?: string
    size?: 'sm' | 'md' | 'lg' | 'xl' | 'wide' | 'nav'
    closeDisabled?: boolean
    presentation?: 'modal' | 'drawer'
    drawerSide?: 'left' | 'right'
    /** Overrides the shared --z-modal layer, e.g. a proof above a form. */
    zIndex?: number
    /** Draws a subtle divider above the footer; drawers default to true. */
    footerBorder?: boolean
  }>(),
  {
    title: '',
    subtitle: '',
    ariaLabel: '',
    ariaDescribedby: '',
    size: 'md',
    closeDisabled: false,
    presentation: 'modal',
    drawerSide: 'right',
    zIndex: undefined,
    footerBorder: undefined,
  }
)

const emit = defineEmits<{
  close: []
}>()

const widths = {
  sm: 'max-w-sm',
  md: 'max-w-md',
  lg: 'max-w-2xl',
  xl: 'max-w-4xl',
  wide: 'max-w-5xl',
  nav: 'max-w-[min(22rem,calc(100vw-24px))]',
}

const titleId = useId()
const subtitleId = useId()
const stackEntry = Symbol('ConsoleModal')
const panel = ref<HTMLElement | null>(null)
const viewportHeight = ref<number | null>(null)
let previouslyFocused: HTMLElement | null = null

function syncViewportHeight() {
  viewportHeight.value = window.visualViewport
    ? Math.round(window.visualViewport.height)
    : null
}

function focusableEls(): HTMLElement[] {
  return panel.value ? tabbableElements(panel.value) : []
}

function onKeydown(e: KeyboardEvent) {
  if (e.isComposing || !props.open || !isTopmostModalDialog(stackEntry)) return
  if (e.key === 'Escape') {
    // A menu or picker inside the dialog already used this Escape to close
    // itself.
    if (e.defaultPrevented) return
    e.preventDefault()
    requestClose()
    return
  }
  if (e.key === 'Tab') {
    // A picker inside the dialog already moved focus for this Tab.
    if (e.defaultPrevented) return
    // Trap focus within the dialog.
    const els = focusableEls()
    if (els.length === 0) {
      e.preventDefault()
      panel.value?.focus()
      return
    }
    const first = els[0]
    const last = els[els.length - 1]
    const active = document.activeElement as HTMLElement
    // A picker panel opened from this dialog handles its own Tab boundary.
    const inside =
      !!panel.value && containsWithFloatingPanels(panel.value, active)
    if (inside && !panel.value?.contains(active)) return
    if (e.shiftKey && (active === first || !inside)) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && active === last) {
      e.preventDefault()
      first.focus()
    }
  }
}

function requestClose(): void {
  if (!props.closeDisabled) emit('close')
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      lockBodyScroll(stackEntry)
      openModalDialog(stackEntry)
      previouslyFocused = document.activeElement as HTMLElement | null
      syncViewportHeight()
      window.addEventListener('keydown', onKeydown)
      window.visualViewport?.addEventListener('resize', syncViewportHeight)
      // Move focus into the dialog once it has rendered.
      nextTick(() => {
        const els = focusableEls()
        ;(els[0] ?? panel.value)?.focus()
      })
    } else {
      closeModalDialog(stackEntry)
      unlockBodyScroll(stackEntry)
      window.removeEventListener('keydown', onKeydown)
      window.visualViewport?.removeEventListener('resize', syncViewportHeight)
      viewportHeight.value = null
      const restoreTarget = previouslyFocused
      previouslyFocused = null
      nextTick(() => restoreTarget?.isConnected && restoreTarget.focus())
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  closeModalDialog(stackEntry)
  unlockBodyScroll(stackEntry)
  window.removeEventListener('keydown', onKeydown)
  window.visualViewport?.removeEventListener('resize', syncViewportHeight)
  const restoreTarget = previouslyFocused
  previouslyFocused = null
  nextTick(() => restoreTarget?.isConnected && restoreTarget.focus())
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div
        v-if="open"
        class="fixed inset-0 flex"
        :style="{ zIndex: zIndex ?? 'var(--z-modal)' }"
        :class="
          presentation === 'drawer'
            ? [
                'items-stretch',
                drawerSide === 'left' ? 'justify-start' : 'justify-end',
              ]
            : 'items-center justify-center p-4'
        "
        :data-drawer-side="presentation === 'drawer' ? drawerSide : undefined"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="title ? titleId : undefined"
        :aria-label="!title ? ariaLabel || undefined : undefined"
        :aria-describedby="subtitle ? subtitleId : ariaDescribedby || undefined"
      >
        <div
          class="absolute inset-0 backdrop-blur-[4px] sm:backdrop-blur-md"
          :style="{ background: 'var(--drawer-backdrop)' }"
          @click="requestClose"
        />
        <div
          ref="panel"
          tabindex="-1"
          class="pencil-surface-strong relative flex max-h-[calc(var(--modal-vh,100dvh)-2rem)] w-full flex-col border border-[var(--border-subtle)] bg-[var(--surface-solid)] texture-paper animate-scale-in focus:outline-hidden"
          data-handdrawn="modal"
          :style="{
            borderRadius: 'var(--shape-overlay)',
            boxShadow: 'var(--overlay-shadow)',
            '--modal-vh': viewportHeight ? `${viewportHeight}px` : undefined,
          }"
          :class="[
            widths[size],
            { 'console-drawer-panel': presentation === 'drawer' },
          ]"
        >
          <!-- Decorative gold-line top accent -->
          <div
            class="shrink-0 h-px w-full"
            style="
              background: linear-gradient(
                90deg,
                transparent 10%,
                var(--dec-gold-line) 40%,
                var(--dec-gold-line) 60%,
                transparent 90%
              );
            "
            aria-hidden="true"
          />
          <header
            v-if="title || subtitle || $slots.header"
            class="shrink-0 px-6 pt-5"
            :class="
              presentation === 'drawer' ? 'pb-5 text-left' : 'text-center'
            "
          >
            <slot
              name="header"
              :title-id="titleId"
              :subtitle-id="subtitleId"
              :close="requestClose"
            >
              <h2
                :id="titleId"
                class="display-title text-xl font-bold text-[var(--text-primary)]"
              >
                {{ title }}
              </h2>
              <p
                v-if="subtitle"
                :id="subtitleId"
                class="mt-1.5 text-sm text-[var(--text-tertiary)]"
              >
                {{ subtitle }}
              </p>
            </slot>
          </header>
          <div
            class="subtle-scroll min-h-0 overflow-y-auto px-6 py-5"
            :class="{ 'flex-1': presentation === 'drawer' }"
          >
            <slot />
          </div>
          <footer
            v-if="$slots.footer"
            class="shrink-0 px-6 pb-6"
            :class="{
              'border-t border-[var(--border-subtle)]':
                footerBorder ?? presentation === 'drawer',
              'pt-5': presentation === 'drawer',
              'pt-4': presentation !== 'drawer' && footerBorder,
            }"
          >
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.console-drawer-panel[data-handdrawn='modal'] {
  height: 100dvh;
  max-height: 100dvh;
  border-radius: 0 !important;
}
.console-drawer-panel {
  padding-block: env(safe-area-inset-top) env(safe-area-inset-bottom);
}
.modal-enter-active .console-drawer-panel,
.modal-leave-active .console-drawer-panel {
  transition: transform 220ms cubic-bezier(0.22, 1, 0.36, 1);
}
.modal-enter-from [data-handdrawn='modal'].console-drawer-panel,
.modal-leave-to [data-handdrawn='modal'].console-drawer-panel {
  transform: translateX(100%);
}
.modal-enter-from[data-drawer-side='left'] .console-drawer-panel,
.modal-leave-to[data-drawer-side='left'] .console-drawer-panel {
  transform: translateX(-100%);
}
html.dark .pencil-surface-strong[data-handdrawn='modal'] {
  background: var(--surface-overlay);
  background-image: none;
  backdrop-filter: none;
}
@media (max-width: 639px) {
  [data-handdrawn='modal']:not(.console-drawer-panel) {
    max-height: calc(
      var(--modal-vh, 100dvh) - 2rem - env(safe-area-inset-top) -
        env(safe-area-inset-bottom)
    );
  }
}
@media (prefers-reduced-motion: reduce) {
  .modal-enter-active,
  .modal-leave-active,
  .modal-enter-active .console-drawer-panel,
  .modal-leave-active .console-drawer-panel {
    transition: none;
  }
}
.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.2s ease;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
</style>
