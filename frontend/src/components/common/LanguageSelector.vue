<template>
  <div ref="root" class="relative" data-hero-canvas-boundary>
    <button
      ref="trigger"
      type="button"
      class="flex h-11 w-11 items-center justify-center rounded-full border border-[var(--border-subtle)] bg-[var(--surface-muted)] text-sm text-[var(--text-secondary)] transition-colors hover:border-[var(--border-strong)] hover:text-[var(--text-primary)] focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--focus-ring)] md:h-auto md:w-auto md:gap-1.5 md:px-3 md:py-1.5"
      :aria-expanded="open"
      aria-haspopup="menu"
      aria-controls="language-selector-menu"
      :aria-label="t('nav.language')"
      @click="toggle"
      @keydown="onTriggerKeydown"
    >
      <svg
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="9" />
        <path
          d="M3 12h18M12 3c2.5 2.7 2.5 15.3 0 18M12 3c-2.5 2.7-2.5 15.3 0 18"
        />
      </svg>
      <span class="hidden md:inline">{{ currentLabel }}</span>
      <svg
        width="12"
        height="12"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.4"
        class="hidden transition-transform duration-200 md:block"
        :class="open ? 'rotate-180' : ''"
        aria-hidden="true"
      >
        <path d="M6 9l6 6 6-6" />
      </svg>
    </button>

    <transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0 -translate-y-1 scale-95"
      leave-active-class="transition duration-150 ease-in"
      leave-to-class="opacity-0 -translate-y-1 scale-95"
    >
      <ul
        v-if="open"
        id="language-selector-menu"
        ref="menu"
        role="menu"
        class="absolute right-0 z-[90] mt-2 w-40 overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-overlay)] p-1 shadow-[0_24px_46px_var(--shadow-color)] backdrop-blur-xl"
        :aria-label="t('nav.language')"
        @keydown="onMenuKeydown"
        @focusout="onMenuFocusout"
      >
        <li v-for="loc in availableLocales" :key="loc.code" role="none">
          <button
            type="button"
            role="menuitemradio"
            class="flex min-h-11 w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-[var(--focus-ring)]"
            :class="
              loc.code === current
                ? 'bg-[var(--accent-soft)] text-[var(--text-primary)]'
                : 'text-[var(--text-secondary)] hover:bg-[var(--surface-muted)] hover:text-[var(--text-primary)]'
            "
            :aria-checked="loc.code === current"
            @click="choose(loc.code)"
          >
            <span aria-hidden="true">{{ loc.flag }}</span>
            <span>{{ loc.name }}</span>
            <svg
              v-if="loc.code === current"
              class="ml-auto text-[var(--accent)]"
              width="15"
              height="15"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2.5"
              aria-hidden="true"
            >
              <path d="M20 6L9 17l-5-5" />
            </svg>
          </button>
        </li>
      </ul>
    </transition>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { onClickOutside } from '@vueuse/core'
import { availableLocales, getLocale, setLocale, type LocaleCode } from '@/i18n'

const { t } = useI18n()
const root = ref<HTMLElement | null>(null)
const trigger = ref<HTMLButtonElement | null>(null)
const menu = ref<HTMLElement | null>(null)
const open = ref(false)
const current = ref<LocaleCode>(getLocale())

const currentLabel = computed(
  () =>
    availableLocales.find((locale) => locale.code === current.value)?.name ?? ''
)

function close(restoreFocus = false) {
  if (!open.value) return
  open.value = false
  if (restoreFocus) nextTick(() => trigger.value?.focus())
}

function focusOption(offset = 0) {
  const options = Array.from(
    menu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') ??
      []
  )
  if (!options.length) return

  const selectedIndex = Math.max(
    0,
    availableLocales.findIndex((locale) => locale.code === current.value)
  )
  const targetIndex = (selectedIndex + offset + options.length) % options.length
  options[targetIndex]?.focus()
}

function openMenu(offset = 0) {
  open.value = true
  nextTick(() => focusOption(offset))
}

function toggle() {
  if (open.value) close()
  else openMenu()
}

function choose(code: LocaleCode) {
  setLocale(code)
  current.value = code
  close(true)
}

function onTriggerKeydown(event: KeyboardEvent) {
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    openMenu()
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    openMenu(-1)
  } else if (event.key === 'Escape') {
    event.preventDefault()
    close(true)
  }
}

function onMenuKeydown(event: KeyboardEvent) {
  const options = Array.from(
    menu.value?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') ??
      []
  )
  const activeIndex = Math.max(
    0,
    options.indexOf(document.activeElement as HTMLButtonElement)
  )

  if (event.key === 'ArrowDown') {
    event.preventDefault()
    options[(activeIndex + 1) % options.length]?.focus()
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    options[(activeIndex - 1 + options.length) % options.length]?.focus()
  } else if (event.key === 'Home') {
    event.preventDefault()
    options[0]?.focus()
  } else if (event.key === 'End') {
    event.preventDefault()
    options[options.length - 1]?.focus()
  } else if (event.key === 'Escape') {
    event.preventDefault()
    close(true)
  } else if (event.key === 'Tab') {
    close()
  }
}

function onMenuFocusout(event: FocusEvent) {
  const nextTarget = event.relatedTarget
  if (!(nextTarget instanceof Node) || !root.value?.contains(nextTarget))
    close()
}

onClickOutside(root, () => close())
</script>
