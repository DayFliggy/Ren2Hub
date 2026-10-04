<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, ExternalLink } from 'lucide-vue-next'

import StatusChip from '@/components/common/StatusChip.vue'
import { useToast } from '@/composables/useToast'
import type { TaskAudioClip } from '@/types/console'

const props = defineProps<{ clip: TaskAudioClip }>()

const { t } = useI18n()
const toast = useToast()

const playbackFailed = ref(false)
const imageFailed = ref(false)

const title = computed(() => props.clip.title || t('relayLogs.untitled'))
const duration = computed(() => {
  const seconds = props.clip.duration
  if (!seconds) return ''
  const minutes = Math.floor(seconds / 60)
  return `${minutes}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`
})

async function copyAudioUrl(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.clip.audio_url)
    toast.success(t('common.copied'))
  } catch {
    toast.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <article
    class="flex gap-4 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4"
    data-task-audio-clip
  >
    <img
      v-if="clip.image_url && !imageFailed"
      :src="clip.image_url"
      :alt="title"
      class="size-20 shrink-0 rounded-lg object-cover"
      @error="imageFailed = true"
    />
    <div class="min-w-0 flex-1">
      <div class="mb-1 flex items-center gap-2">
        <span class="truncate text-sm font-semibold text-[var(--text-primary)]">
          {{ title }}
        </span>
        <StatusChip v-if="duration" tone="neutral" class="shrink-0">
          {{ duration }}
        </StatusChip>
      </div>
      <p
        v-if="clip.tags"
        class="mb-2 truncate text-xs text-[var(--text-tertiary)]"
      >
        {{ clip.tags }}
      </p>
      <div v-if="playbackFailed" class="flex flex-wrap items-center gap-2">
        <span role="alert" class="text-xs text-[var(--status-danger-text)]">
          {{ t('relayLogs.audioFailed') }}
        </span>
        <a
          :href="clip.audio_url"
          target="_blank"
          rel="noopener noreferrer"
          class="focus-ring inline-flex items-center gap-1 rounded-md border border-[var(--border-default)] px-2 py-1 text-xs text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
        >
          <ExternalLink class="size-3" aria-hidden="true" />
          {{ t('relayLogs.openInNewTab') }}
        </a>
        <button
          type="button"
          class="focus-ring inline-flex items-center gap-1 rounded-md border border-[var(--border-default)] px-2 py-1 text-xs text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
          @click="copyAudioUrl"
        >
          <Copy class="size-3" aria-hidden="true" />
          {{ t('relayLogs.copyResultLink') }}
        </button>
      </div>
      <audio
        v-else
        :src="clip.audio_url"
        controls
        preload="none"
        class="h-9 w-full"
        @error="playbackFailed = true"
      />
    </div>
  </article>
</template>
