<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileText, MusicIcon, VideoIcon } from 'lucide-vue-next'

import { getTaskArtifacts } from '@/api/taskArtifacts'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import type {
  RelayTaskLogItem,
  TaskArtifact,
  TaskArtifactsResult,
} from '@/types/console'

import TaskArtifactCard from './TaskArtifactCard.vue'
import TaskAudioClipCard from './TaskAudioClipCard.vue'
import { taskPreviewMode } from './taskArtifacts'

const props = defineProps<{ task: RelayTaskLogItem }>()

const { t } = useI18n()
const request = useLatestRequest()

const open = ref(false)
const loading = ref(false)
const failure = ref('')
// null: the gateway reported artifact_not_found for this task.
const result = ref<TaskArtifactsResult | null>(null)

const mode = computed(() => taskPreviewMode(props.task))

const trigger = computed(() => {
  if (mode.value === 'legacy-suno')
    return { icon: MusicIcon, label: t('relayLogs.previewAudio') }
  if (mode.value === 'legacy-video')
    return { icon: VideoIcon, label: t('relayLogs.previewVideo') }
  return { icon: FileText, label: t('relayLogs.artifacts') }
})

const dialogTitle = computed(() => {
  if (mode.value === 'legacy-suno') return t('relayLogs.audioPreview')
  if (mode.value === 'legacy-video') return t('relayLogs.preview')
  return t('relayLogs.artifacts')
})

// Plugin artifacts win; a pre-plugin video is shown through the same card
// under the gateway's legacy "video" key when the task has none.
const artifacts = computed<TaskArtifact[]>(() => {
  const value = result.value
  if (!value) return []
  if (value.artifacts.length || !value.legacy_content_url)
    return value.artifacts
  return [
    { key: 'video', type: 'video', content_url: value.legacy_content_url },
  ]
})
const clips = computed(() => result.value?.legacy_audio_clips ?? [])

// Signed content URLs are short-lived, so every opening fetches fresh ones.
async function load(): Promise<void> {
  loading.value = true
  failure.value = ''
  result.value = null
  const outcome = await request.run((signal) =>
    getTaskArtifacts(props.task.task_id, signal)
  )
  if (outcome.stale) return
  loading.value = false
  if (outcome.ok) {
    result.value = outcome.value
    return
  }
  failure.value =
    outcome.error instanceof Error
      ? outcome.error.message
      : String(outcome.error)
}

function openPreview(): void {
  open.value = true
  void load()
}

function closePreview(): void {
  request.cancel()
  open.value = false
  loading.value = false
}
</script>

<template>
  <span
    v-if="mode === 'discarded'"
    class="cursor-help text-xs text-[var(--text-tertiary)] underline decoration-dotted underline-offset-2"
    :title="t('relayLogs.resultDiscardedHint')"
    data-task-result-discarded
  >
    {{ t('relayLogs.resultDiscarded') }}
  </span>
  <span v-else-if="mode === 'none'" class="text-xs text-[var(--text-tertiary)]"
    >-</span
  >
  <template v-else>
    <button
      type="button"
      class="focus-ring inline-flex items-center gap-1 rounded-md text-xs underline-offset-2 hover:underline"
      :class="
        mode === 'artifacts'
          ? 'border border-[var(--border-default)] px-2 py-1 font-semibold text-[var(--text-secondary)] hover:text-[var(--text-primary)]'
          : 'text-[var(--accent-text)]'
      "
      data-task-artifacts-trigger
      @click="openPreview"
    >
      <component :is="trigger.icon" class="size-3.5" aria-hidden="true" />
      {{ trigger.label }}
    </button>

    <ConsoleModal
      :open="open"
      :title="dialogTitle"
      :subtitle="task.task_id"
      :size="mode === 'artifacts' ? 'xl' : 'md'"
      @close="closePreview"
    >
      <div v-if="loading" aria-busy="true">
        <span class="sr-only">{{ t('relayLogs.artifactsLoading') }}</span>
        <SkeletonBlock rounded="xl" class="aspect-video min-h-48 w-full" />
      </div>
      <ErrorBanner v-else-if="failure" :message="failure" @retry="load" />
      <div
        v-else-if="artifacts.length"
        class="grid gap-3"
        :class="{ 'lg:grid-cols-2': artifacts.length > 1 }"
      >
        <TaskArtifactCard
          v-for="artifact in artifacts"
          :key="artifact.key"
          :artifact="artifact"
        />
      </div>
      <div v-else-if="clips.length" class="sibling-space-y-3">
        <TaskAudioClipCard
          v-for="(clip, index) in clips"
          :key="clip.clip_id || clip.id || index"
          :clip="clip"
        />
      </div>
      <div
        v-else
        class="flex flex-col items-center gap-2 py-8 text-[var(--text-tertiary)]"
        data-task-artifacts-empty
      >
        <FileText class="size-6" aria-hidden="true" />
        <p class="text-sm">{{ t('relayLogs.artifactsEmpty') }}</p>
      </div>
      <template #footer>
        <div class="flex justify-end">
          <ConsoleButton variant="secondary" @click="closePreview">
            {{ t('common.close') }}
          </ConsoleButton>
        </div>
      </template>
    </ConsoleModal>
  </template>
</template>
