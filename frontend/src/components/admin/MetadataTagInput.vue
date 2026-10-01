<script setup lang="ts">
/** Free-text tag input: Enter or comma commits, Backspace on empty removes. */
import { ref } from 'vue'
import { X } from 'lucide-vue-next'

defineProps<{
  label: string
  removeLabel: string
  placeholder?: string
  disabled?: boolean
}>()
const model = defineModel<string[]>({ required: true })
const draft = ref('')

function commit() {
  const parts = draft.value
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)
  draft.value = ''
  if (parts.length) model.value = [...model.value, ...parts]
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Enter' || event.key === ',') {
    event.preventDefault()
    commit()
  } else if (event.key === 'Backspace' && !draft.value && model.value.length) {
    model.value = model.value.slice(0, -1)
  }
}

function remove(index: number) {
  model.value = model.value.filter((_, i) => i !== index)
}

/** Commits pending text so a save never drops what the user typed. */
defineExpose({ commit })
</script>
<template>
  <div
    class="pencil-control flex min-h-10 flex-wrap items-center gap-1.5 rounded-xl border border-[var(--outline-variant)] bg-[var(--surface-container-low)] px-2 py-1.5"
    data-handdrawn="control"
  >
    <span
      v-for="(tag, index) in model"
      :key="index"
      class="inline-flex items-center gap-1 rounded-md bg-[var(--surface-muted)] py-0.5 pl-2 pr-1 text-xs text-[var(--text-secondary)]"
    >
      {{ tag }}
      <button
        type="button"
        class="rounded p-0.5 text-[var(--text-tertiary)] hover:text-[var(--text-primary)] focus-ring disabled:opacity-40"
        :aria-label="`${removeLabel} ${tag}`"
        :disabled="disabled"
        @click="remove(index)"
      >
        <X :size="12" aria-hidden="true" />
      </button>
    </span>
    <input
      v-model="draft"
      class="min-w-24 flex-1 border-0 bg-transparent px-1 text-sm text-[var(--text-primary)] outline-none placeholder:text-[var(--text-tertiary)]"
      :aria-label="label"
      :placeholder="model.length ? '' : placeholder"
      :disabled="disabled"
      @keydown="onKeydown"
      @blur="commit"
    />
  </div>
</template>
