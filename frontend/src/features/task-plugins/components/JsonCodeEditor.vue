<script setup lang="ts">
// Upstream @/components/json-code-editor on a plain textarea: a line-number
// gutter, a validity mark, copy and format.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Braces, CircleAlert, CircleCheck } from 'lucide-vue-next'

import IconButton from '@/components/common/IconButton.vue'
import CopyButton from './CopyButton.vue'

const props = defineProps<{ id: string; label: string }>()
const model = defineModel<string>({ required: true })
const { t } = useI18n()
const gutter = ref<HTMLElement | null>(null)

const lineCount = computed(() => model.value.split('\n').length)
const parsed = computed<{ ok: true; value: unknown } | { ok: false }>(() => {
  try {
    return { ok: true, value: JSON.parse(model.value) }
  } catch {
    return { ok: false }
  }
})

function format() {
  if (parsed.value.ok) model.value = JSON.stringify(parsed.value.value, null, 2)
}

function syncScroll(event: Event) {
  if (gutter.value)
    gutter.value.scrollTop = (event.target as HTMLTextAreaElement).scrollTop
}
</script>

<template>
  <div
    class="overflow-hidden rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)]"
    data-json-editor
  >
    <div
      class="flex items-center justify-between gap-2 border-b border-[var(--border-subtle)] px-3 py-1"
    >
      <span
        role="status"
        class="inline-flex items-center gap-1.5 text-xs"
        :class="
          parsed.ok
            ? 'text-[var(--status-success-text)]'
            : 'text-[var(--status-danger-text)]'
        "
      >
        <CircleCheck v-if="parsed.ok" :size="14" aria-hidden="true" />
        <CircleAlert v-else :size="14" aria-hidden="true" />
        {{
          parsed.ok
            ? t('pluginExtensions.rp.jsonValid')
            : t('pluginExtensions.rp.jsonInvalid')
        }}
      </span>
      <span class="flex items-center gap-0.5">
        <CopyButton :value="model" :label="t('pluginExtensions.rp.copy')" />
        <IconButton
          :label="t('pluginExtensions.rp.formatJson')"
          :disabled="!parsed.ok"
          @click="format"
        >
          <Braces :size="14" aria-hidden="true" />
        </IconButton>
      </span>
    </div>
    <div class="flex h-56">
      <div
        ref="gutter"
        aria-hidden="true"
        class="w-10 shrink-0 select-none overflow-hidden border-r border-[var(--border-subtle)] py-2 pr-2 text-right font-mono text-xs leading-5 text-[var(--text-tertiary)]"
      >
        <div v-for="line in lineCount" :key="line">{{ line }}</div>
      </div>
      <textarea
        :id="props.id"
        v-model="model"
        :aria-label="label"
        :aria-invalid="!parsed.ok || undefined"
        spellcheck="false"
        wrap="off"
        class="subtle-scroll min-w-0 flex-1 resize-none bg-transparent px-3 py-2 font-mono text-xs leading-5 text-[var(--text-primary)] outline-none"
        @scroll="syncScroll"
      />
    </div>
  </div>
</template>
