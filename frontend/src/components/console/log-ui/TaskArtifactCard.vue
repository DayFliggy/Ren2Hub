<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Download,
  FileIcon,
  ImageIcon,
  MusicIcon,
  RefreshCw,
  VideoIcon,
} from 'lucide-vue-next'

import type { TaskArtifact, TaskArtifactType } from '@/types/console'

const props = defineProps<{ artifact: TaskArtifact }>()

const { t } = useI18n()

const ICONS = {
  image: ImageIcon,
  video: VideoIcon,
  audio: MusicIcon,
  file: FileIcon,
} satisfies Record<TaskArtifactType, unknown>

const LABEL_KEYS: Record<TaskArtifactType, string> = {
  image: 'relayLogs.artifactImage',
  video: 'relayLogs.artifactVideo',
  audio: 'relayLogs.artifactAudio',
  file: 'relayLogs.artifactFile',
}

const icon = computed(() => ICONS[props.artifact.type])

// A failed media element is remounted on retry so the browser refetches it.
const mediaFailed = ref(false)
const mediaRevision = ref(0)

function retryMedia(): void {
  mediaFailed.value = false
  mediaRevision.value += 1
}
</script>

<template>
  <article
    class="flex min-w-0 flex-col gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3"
    data-task-artifact
  >
    <header class="min-w-0">
      <p
        class="flex min-w-0 items-center gap-1.5 text-sm font-semibold text-[var(--text-primary)]"
      >
        <component :is="icon" class="size-4 shrink-0" aria-hidden="true" />
        <span class="truncate">{{ t(LABEL_KEYS[artifact.type]) }}</span>
      </p>
      <p class="mt-0.5 truncate font-mono text-xs text-[var(--text-tertiary)]">
        {{ artifact.key }}
      </p>
      <p
        v-if="artifact.mime_type"
        class="truncate font-mono text-[0.6875rem] text-[var(--text-tertiary)]"
      >
        {{ artifact.mime_type }}
      </p>
    </header>

    <div
      v-if="mediaFailed"
      role="alert"
      class="flex flex-wrap items-center gap-2 rounded-lg border border-[var(--status-danger)] bg-[var(--status-danger-soft)] px-3 py-2"
    >
      <span class="flex-1 text-xs text-[var(--status-danger-text)]">
        {{ t('relayLogs.mediaFailed') }}
      </span>
      <button
        type="button"
        class="focus-ring inline-flex items-center gap-1 rounded-md border border-[var(--border-default)] bg-[var(--surface-solid)] px-2 py-1 text-xs text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
        @click="retryMedia"
      >
        <RefreshCw class="size-3" aria-hidden="true" />
        {{ t('common.retry') }}
      </button>
    </div>
    <div v-else-if="artifact.type !== 'file'" :key="mediaRevision">
      <img
        v-if="artifact.type === 'image'"
        :src="artifact.content_url"
        :alt="artifact.key"
        loading="lazy"
        class="max-h-[60vh] w-full rounded-md object-contain"
        @error="mediaFailed = true"
      />
      <video
        v-else-if="artifact.type === 'video'"
        :src="artifact.content_url"
        :aria-label="`${t(LABEL_KEYS[artifact.type])}: ${artifact.key}`"
        controls
        preload="metadata"
        class="max-h-[60vh] w-full rounded-md bg-black"
        @error="mediaFailed = true"
      />
      <audio
        v-else
        :src="artifact.content_url"
        :aria-label="`${t(LABEL_KEYS[artifact.type])}: ${artifact.key}`"
        controls
        preload="none"
        class="w-full"
        @error="mediaFailed = true"
      />
    </div>
    <div
      v-else
      class="flex min-h-20 items-center justify-center rounded-md bg-[var(--surface-muted)] text-[var(--text-tertiary)]"
    >
      <component :is="icon" class="size-6" aria-hidden="true" />
    </div>

    <a
      :href="artifact.content_url"
      :download="artifact.key"
      target="_blank"
      rel="noopener noreferrer"
      :aria-label="t('relayLogs.artifactDownloadNamed', { key: artifact.key })"
      class="focus-ring inline-flex w-fit items-center gap-1.5 rounded-lg border border-[var(--border-default)] px-3 py-1.5 text-xs font-semibold text-[var(--text-secondary)] hover:text-[var(--text-primary)]"
    >
      <Download class="size-3.5" aria-hidden="true" />
      {{ t('relayLogs.artifactDownload') }}
    </a>
  </article>
</template>
