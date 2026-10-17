<script setup lang="ts">
import './admin.css'
import { useI18n } from 'vue-i18n'
import { reactive, watch } from 'vue'
import type { ModelCapabilities } from '@/api/modelManifest'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { modelCapabilitiesMessages } from '@/i18n/modelCapabilities'

const card = defineModel<ModelCapabilities | null | undefined>()
const { t } = useI18n({
  useScope: 'local',
  messages: modelCapabilitiesMessages,
})
const lists = [
  'reasoning_efforts',
  'input_modalities',
  'output_modalities',
] as const
const limits = ['context_limit', 'output_limit'] as const
const drafts = reactive<Record<(typeof lists)[number], string>>({
  reasoning_efforts: '',
  input_modalities: '',
  output_modalities: '',
})
watch(
  card,
  (value) => {
    for (const key of lists) {
      const parsedDraft = drafts[key]
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean)
      if (JSON.stringify(parsedDraft) !== JSON.stringify(value?.[key] ?? []))
        drafts[key] = value?.[key]?.join(', ') ?? ''
      if (value?.[key] == null) drafts[key] = ''
    }
  },
  { immediate: true }
)

function change(update: Partial<ModelCapabilities>) {
  card.value = {
    reasoning_efforts: null,
    input_modalities: null,
    output_modalities: null,
    context_limit: null,
    output_limit: null,
    tool_call: null,
    source: null,
    updated_at: 0,
    ...card.value,
    ...update,
  }
}
function clear() {
  change({
    reasoning_efforts: null,
    input_modalities: null,
    output_modalities: null,
    context_limit: null,
    output_limit: null,
    tool_call: null,
    source: null,
  })
}
</script>

<template>
  <section data-model-capabilities class="sibling-space-y-3">
    <h3 class="text-sm font-semibold text-[var(--text-primary)]">
      {{ t('title') }}
    </h3>
    <p class="text-xs text-[var(--text-secondary)]">{{ t('hint') }}</p>
    <div class="grid gap-4 sm:grid-cols-2">
      <div v-for="key in lists" :key="key" class="admin-field sm:col-span-2">
        <label :for="`capability-${key}-state`">{{ t(key) }}</label>
        <select
          :id="`capability-${key}-state`"
          :value="card?.[key] == null ? 'unknown' : 'known'"
          @change="
            change({
              [key]:
                ($event.target as HTMLSelectElement).value === 'unknown'
                  ? null
                  : [],
            })
          "
        >
          <option value="unknown">{{ t('unknown') }}</option>
          <option value="known">{{ t('known') }}</option>
        </select>
        <input
          v-if="card?.[key] != null"
          v-model="drafts[key]"
          :aria-label="t(key)"
          :placeholder="t('listHint')"
          @input="
            change({
              [key]: drafts[key]
                .split(',')
                .map((value) => value.trim())
                .filter(Boolean),
            })
          "
        />
      </div>
      <label v-for="key in limits" :key="key" class="admin-field">
        <span>{{ t(key) }}</span>
        <input
          type="number"
          min="1"
          step="1"
          :aria-label="t(key)"
          :value="card?.[key] ?? ''"
          :placeholder="t('unknown')"
          @input="
            change({
              [key]:
                ($event.target as HTMLInputElement).value === ''
                  ? null
                  : Number(($event.target as HTMLInputElement).value),
            })
          "
        />
      </label>
      <label class="admin-field">
        <span>{{ t('tool_call') }}</span>
        <select
          :aria-label="t('tool_call')"
          :value="card?.tool_call == null ? 'unknown' : String(card.tool_call)"
          @change="
            change({
              tool_call:
                ($event.target as HTMLSelectElement).value === 'unknown'
                  ? null
                  : ($event.target as HTMLSelectElement).value === 'true',
            })
          "
        >
          <option value="unknown">{{ t('unknown') }}</option>
          <option value="true">{{ t('yes') }}</option>
          <option value="false">{{ t('no') }}</option>
        </select>
      </label>
      <label class="admin-field"
        ><span>{{ t('source') }}</span>
        <input
          :aria-label="t('source')"
          maxlength="255"
          :value="card?.source ?? ''"
          @input="
            change({
              source: ($event.target as HTMLInputElement).value || null,
            })
          "
        />
      </label>
    </div>
    <ConsoleButton variant="ghost" size="sm" @click="clear">{{
      t('clear')
    }}</ConsoleButton>
  </section>
</template>
