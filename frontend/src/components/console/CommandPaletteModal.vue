<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import ConsoleModal from '@/components/common/ConsoleModal.vue'
import SearchInput from '@/components/common/SearchInput.vue'

interface PaletteEntry {
  route: string
  labelKey: string
  icon: string
}

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n()
const router = useRouter()

const entries: PaletteEntry[] = [
  {
    route: 'dashboard',
    labelKey: 'nav.dashboard',
    icon: 'M4 4h7v7H4zM13 4h7v4h-7zM13 11h7v9h-7zM4 14h7v6H4z',
  },
  {
    route: 'keys',
    labelKey: 'nav.keys',
    icon: 'M14 10a4 4 0 1 0-4 4c.4 0 .8-.06 1.16-.2L13 15.66V18h2.34l1.83-1.83v-2.34L15.34 12h-2.2l-.7-.7c.08-.34.13-.7.13-1.06Z',
  },
  {
    route: 'wallet',
    labelKey: 'nav.wallet',
    icon: 'M4 7h16v12H4zM4 7l2-3h12l2 3M15 13h3',
  },
  {
    route: 'logs',
    labelKey: 'nav.logs',
    icon: 'M6 3h9l4 4v14H6zM14 3v5h5M9 12h6M9 16h6',
  },
  {
    route: 'subscription',
    labelKey: 'nav.subscription',
    icon: 'M12 3l2.7 5.6 6.1.8-4.5 4.2 1.1 6-5.4-3-5.4 3 1.1-6L3.2 9.4l6.1-.8z',
  },
  {
    route: 'invite',
    labelKey: 'nav.invite',
    icon: 'M16 11a3 3 0 1 0-3-3M8 11a3 3 0 1 0-3-3M2 20c0-3 2.5-5 6-5M22 20c0-3-2.5-5-6-5M12 14c-3.5 0-6 2-6 5v1h12v-1c0-3-2.5-5-6-5Z',
  },
  {
    route: 'settings',
    labelKey: 'nav.settings',
    icon: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm8-3a8 8 0 0 1-.15 1.57l2.1 1.63-2 3.46-2.47-1a8.1 8.1 0 0 1-2.72 1.57L14.3 22H9.7l-.46-2.77a8.1 8.1 0 0 1-2.72-1.57l-2.47 1-2-3.46 2.1-1.63a8 8 0 0 1 0-3.14L2.05 8.8l2-3.46 2.47 1a8.1 8.1 0 0 1 2.72-1.57L9.7 2h4.6l.46 2.77a8.1 8.1 0 0 1 2.72 1.57l2.47-1 2 3.46-2.1 1.63c.1.5.15 1.02.15 1.57Z',
  },
]

const query = ref('')
const inputWrap = ref<HTMLElement | null>(null)

const filtered = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return entries
  return entries.filter((e) => t(e.labelKey).toLowerCase().includes(q))
})

function go(routeName: string) {
  emit('close')
  router.push({ name: routeName })
}

function onEnter() {
  const first = filtered.value[0]
  if (first) go(first.route)
}

watch(
  () => props.open,
  async (open) => {
    if (!open) return
    query.value = ''
    await nextTick()
    inputWrap.value?.querySelector('input')?.focus()
  }
)
</script>

<template>
  <ConsoleModal
    :open="open"
    :aria-label="t('nav.search')"
    @close="emit('close')"
  >
    <div ref="inputWrap" @keydown.enter.prevent="onEnter">
      <SearchInput
        v-model="query"
        :placeholder="t('nav.search')"
        :aria-label="t('nav.search')"
        name="command-search"
      />
    </div>
    <div class="subtle-scroll mt-3 max-h-80 overflow-y-auto">
      <button
        v-for="entry in filtered"
        :key="entry.route"
        type="button"
        class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-left text-sm text-[var(--text-primary)] transition-colors hover:bg-[var(--surface-muted)]"
        @click="go(entry.route)"
      >
        <svg
          width="15"
          height="15"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          class="shrink-0 text-[var(--text-tertiary)]"
        >
          <path :d="entry.icon" />
        </svg>
        {{ t(entry.labelKey) }}
      </button>
      <p
        v-if="!filtered.length"
        class="px-3 py-6 text-center text-sm text-[var(--text-tertiary)]"
      >
        {{ t('common.none') }}
      </p>
    </div>
  </ConsoleModal>
</template>
