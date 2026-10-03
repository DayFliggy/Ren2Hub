<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, ChevronLeft, ChevronRight, Plus } from 'lucide-vue-next'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import IconButton from '@/components/common/IconButton.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { metadataApi } from '@/api/adminManagement'
import { useToast } from '@/composables/useToast'
import { modelMetadataSyncMessages } from '@/i18n/modelMetadataSync'

// Upstream DEFAULT_PAGE_SIZE; pagination is client-side over the full list.
const PAGE_SIZE = 20

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; create: [name: string]; sync: [] }>()
const { t } = useI18n({
  useScope: 'local',
  messages: modelMetadataSyncMessages,
})
const toast = useToast()
const models = ref<string[]>([])
const search = ref('')
const page = ref(1)
const loading = ref(false)
const error = ref('')
let controller: AbortController | undefined

const filtered = computed(() => {
  const keyword = search.value.trim().toLocaleLowerCase()
  return models.value.filter((name) =>
    name.toLocaleLowerCase().includes(keyword)
  )
})
const pageCount = computed(() =>
  Math.max(1, Math.ceil(filtered.value.length / PAGE_SIZE))
)
const visible = computed(() =>
  filtered.value.slice((page.value - 1) * PAGE_SIZE, page.value * PAGE_SIZE)
)
const rangeStart = computed(() =>
  filtered.value.length ? (page.value - 1) * PAGE_SIZE + 1 : 0
)
const rangeEnd = computed(() =>
  Math.min(page.value * PAGE_SIZE, filtered.value.length)
)

watch(search, () => {
  page.value = 1
})
watch(
  () => props.open,
  (open, _, cleanup) => {
    cleanup(() => controller?.abort())
    if (!open) return
    search.value = ''
    page.value = 1
    void load()
  },
  { immediate: true }
)

async function load() {
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  try {
    const data = await metadataApi.missing(current.signal)
    if (current.signal.aborted) return
    models.value = data
    page.value = 1
  } catch (cause) {
    if (!current.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}
async function copyName(name: string) {
  try {
    await navigator.clipboard.writeText(name)
    toast.success(t('copied'))
  } catch {
    toast.error(t('copyFailed'))
  }
}
</script>
<template>
  <ConsoleModal
    :open="open"
    :title="t('missingTitle')"
    :subtitle="t('missingSubtitle')"
    size="lg"
    @close="emit('close')"
  >
    <div v-if="loading" class="grid gap-2" role="status">
      <span class="sr-only">{{ t('loading') }}</span>
      <SkeletonBlock v-for="i in 5" :key="i" rounded="lg" class="h-12" />
    </div>
    <div
      v-else-if="error"
      class="flex flex-col items-center justify-center px-6 py-12 text-center"
      role="alert"
    >
      <AlertTriangle :size="28" class="text-[var(--status-danger-text)]" />
      <p class="mt-3 font-semibold text-[var(--text-primary)]">
        {{ t('loadFailed') }}
      </p>
      <p class="mt-1 max-w-md text-sm text-[var(--text-tertiary)]">
        {{ error }}
      </p>
      <ConsoleButton class="mt-5" variant="secondary" @click="load">
        {{ t('retry') }}
      </ConsoleButton>
    </div>
    <EmptyState
      v-else-if="!models.length"
      :title="t('noMissing')"
      :hint="t('allConfigured')"
    />
    <div v-else class="grid gap-3">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <span
          class="whitespace-nowrap text-sm tabular-nums text-[var(--text-tertiary)]"
          aria-live="polite"
        >
          {{
            t('showingRange', {
              start: rangeStart,
              end: rangeEnd,
              total: filtered.length,
            })
          }}
        </span>
        <SearchInput
          v-model="search"
          :placeholder="t('searchModels')"
          :aria-label="t('searchMissing')"
          name="missing-model-search"
          class="w-full sm:w-56"
        />
      </div>
      <EmptyState
        v-if="!filtered.length"
        :title="t('noMatches')"
        :hint="t('adjustMissingSearch')"
      />
      <div
        v-else
        class="overflow-hidden rounded-lg border border-[var(--border-subtle)]"
      >
        <ul class="divide-y divide-[var(--border-subtle)]">
          <li
            v-for="name in visible"
            :key="name"
            class="flex items-center justify-between gap-3 p-3"
          >
            <button
              type="button"
              class="min-w-0 flex-1 text-left focus-ring"
              :title="t('clickToCopy')"
              :aria-label="`${t('clickToCopy')} ${name}`"
              @click="copyName(name)"
            >
              <StatusChip class="max-w-full cursor-copy">
                <span class="break-all font-mono text-[var(--text-primary)]">{{
                  name
                }}</span>
              </StatusChip>
            </button>
            <ConsoleButton
              size="sm"
              class="shrink-0"
              @click="emit('create', name)"
            >
              <Plus :size="16" aria-hidden="true" />{{ t('configure') }}
            </ConsoleButton>
          </li>
        </ul>
        <div
          class="flex items-center justify-between gap-3 border-t border-[var(--border-subtle)] bg-[var(--surface-solid)] px-3 py-2 text-sm"
        >
          <span class="text-[var(--text-tertiary)]">
            {{ t('pageOf', { current: page, total: pageCount }) }}
          </span>
          <div v-if="filtered.length > PAGE_SIZE" class="flex gap-1">
            <IconButton
              :label="t('previousPage')"
              :disabled="page <= 1"
              @click="page--"
            >
              <ChevronLeft :size="16" />
            </IconButton>
            <IconButton
              :label="t('nextPage')"
              :disabled="page >= pageCount"
              @click="page++"
            >
              <ChevronRight :size="16" />
            </IconButton>
          </div>
        </div>
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end gap-2">
        <ConsoleButton variant="ghost" @click="emit('close')">
          {{ t('close') }}
        </ConsoleButton>
        <ConsoleButton variant="secondary" @click="emit('sync')">
          {{ t('syncMissing') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
