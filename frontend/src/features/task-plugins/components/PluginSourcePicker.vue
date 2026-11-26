<script setup lang="ts">
// Upstream components/plugin-source-picker.tsx: drop zone for the plugin .js
// file. The native input stays labelled and sr-only behind a real button.
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { FileCode2, FolderOpen, Upload } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'

defineProps<{ fileName: string }>()
const emit = defineEmits<{ select: [file: File] }>()
const { t } = useI18n()
const input = ref<HTMLInputElement | null>(null)
const dragActive = ref(false)

function pick(file?: File | null) {
  if (file) emit('select', file)
}
function onDrop(event: DragEvent) {
  dragActive.value = false
  pick(event.dataTransfer?.files[0])
}
function onChange(event: Event) {
  const target = event.target as HTMLInputElement
  pick(target.files?.[0])
  // Allow re-picking the same file after an error.
  target.value = ''
}
</script>

<template>
  <div
    class="flex flex-col items-center gap-2 rounded-xl border [border-style:var(--row-divider-style)] px-4 py-6 text-center transition-colors"
    :class="
      dragActive
        ? 'border-[var(--accent)] bg-[var(--accent-soft)]'
        : 'border-[var(--border-default)] bg-[var(--surface-muted)]'
    "
    @dragover.prevent="dragActive = true"
    @dragleave="dragActive = false"
    @drop.prevent="onDrop"
  >
    <FileCode2
      v-if="fileName"
      :size="20"
      class="text-[var(--text-tertiary)]"
      aria-hidden="true"
    />
    <Upload
      v-else
      :size="20"
      class="text-[var(--text-tertiary)]"
      aria-hidden="true"
    />
    <div class="sibling-space-y-0.5">
      <p class="break-all text-sm font-medium">
        {{ fileName || t('taskPlugins.dropFile') }}
      </p>
      <p class="text-xs text-[var(--text-tertiary)]">
        {{ t('taskPlugins.fileHint') }}
      </p>
    </div>
    <ConsoleButton variant="secondary" size="sm" @click="input?.click()">
      <FolderOpen :size="14" aria-hidden="true" />
      {{
        fileName ? t('taskPlugins.chooseAnother') : t('taskPlugins.chooseFile')
      }}
    </ConsoleButton>
    <label class="sr-only" for="task-plugin-file">{{
      t('taskPlugins.jsFile')
    }}</label>
    <input
      id="task-plugin-file"
      ref="input"
      type="file"
      accept=".js,text/javascript"
      class="sr-only"
      @change="onChange"
    />
  </div>
</template>
