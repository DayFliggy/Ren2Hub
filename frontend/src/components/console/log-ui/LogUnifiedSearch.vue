<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { onClickOutside } from '@vueuse/core'
import { ChevronDown, Search, X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import { useFloatingPanel } from '@/composables/useFloatingPanel'

export interface LogSearchField {
  key: string
  label: string
  placeholder: string
  maxlength?: number
  inputName?: string
}

const props = withDefaults(
  defineProps<{
    fields: LogSearchField[]
    values: Record<string, string>
    /** Optional explicit field key controlled by the parent. */
    modelField?: string
    /** Input name forwarded to the active search input when field has none. */
    inputName?: string
  }>(),
  {
    modelField: '',
    inputName: 'log-unified-search',
  }
)

const emit = defineEmits<{
  'update:value': [key: string, value: string]
  'update:modelField': [key: string]
  clear: [key: string]
}>()

const { t } = useI18n()

const internalField = ref(props.modelField || props.fields[0]?.key || '')
const dropdownOpen = ref(false)
const triggerRef = ref<HTMLButtonElement | null>(null)
const menuRef = ref<HTMLElement | null>(null)
const inputRef = ref<HTMLInputElement | null>(null)
const listboxId = useId()

const { style: menuStyle, scope: menuScope } = useFloatingPanel(
  triggerRef,
  menuRef,
  dropdownOpen,
  { align: 'start', width: 'min', gap: 6 }
)

watch(
  () => props.modelField,
  (next) => {
    if (next && props.fields.some((f) => f.key === next)) {
      internalField.value = next
    }
  }
)

watch(
  () => props.fields.map((f) => f.key).join('|'),
  () => {
    if (!props.fields.some((f) => f.key === internalField.value)) {
      internalField.value = props.fields[0]?.key ?? ''
      emit('update:modelField', internalField.value)
    }
  },
  { immediate: true }
)

const activeField = computed<LogSearchField>(
  () =>
    props.fields.find((f) => f.key === internalField.value) ??
    props.fields[0] ?? {
      key: '',
      label: '',
      placeholder: '',
    }
)

const activeValue = computed(() => props.values[activeField.value.key] ?? '')

const hasMultipleFields = computed(() => props.fields.length > 1)

function selectField(key: string) {
  internalField.value = key
  emit('update:modelField', key)
  dropdownOpen.value = false
  nextTick(() => inputRef.value?.focus())
}

function onInput(event: Event) {
  const value = (event.target as HTMLInputElement).value
  emit('update:value', activeField.value.key, value)
}

function clearActive() {
  emit('update:value', activeField.value.key, '')
  emit('clear', activeField.value.key)
  nextTick(() => inputRef.value?.focus())
}

function onMenuKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    dropdownOpen.value = false
    triggerRef.value?.focus()
  }
}

onClickOutside(menuRef, () => (dropdownOpen.value = false), {
  ignore: [triggerRef],
})
</script>

<template>
  <div
    data-log-unified-search
    class="log-unified-search flex h-11 min-w-0 items-center rounded-[var(--shape-control)] border border-[var(--border-default)] bg-[var(--surface-solid)] transition-[border-color,box-shadow] focus-within:border-[var(--accent)] focus-within:shadow-[var(--focus-halo)] sm:h-10"
  >
    <template v-if="hasMultipleFields">
      <button
        ref="triggerRef"
        type="button"
        role="combobox"
        data-log-search-field-trigger
        :aria-label="t('logs.searchFieldLabel')"
        :aria-expanded="dropdownOpen"
        :aria-controls="listboxId"
        aria-haspopup="listbox"
        class="inline-flex h-full shrink-0 items-center gap-1.5 rounded-l-[calc(var(--shape-control)-1px)] border-r border-[var(--border-subtle)] px-3 text-xs font-medium text-[var(--text-secondary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-visible:outline-hidden sm:text-sm"
        @click="dropdownOpen = !dropdownOpen"
        @keydown="onMenuKeydown"
      >
        <span class="max-w-28 truncate">{{ activeField.label }}</span>
        <ChevronDown
          :size="14"
          aria-hidden="true"
          class="shrink-0 text-[var(--text-tertiary)] transition-transform duration-150"
          :class="{ 'rotate-180': dropdownOpen }"
        />
      </button>

      <Teleport to="body">
        <div
          v-if="dropdownOpen"
          :id="listboxId"
          ref="menuRef"
          role="listbox"
          data-handdrawn="menu"
          :data-handdrawn-scope="menuScope"
          :aria-label="t('logs.searchFieldLabel')"
          :style="menuStyle"
          class="min-w-44 rounded-xl border border-[var(--overlay-border)] bg-[var(--surface-overlay)] p-1 shadow-[var(--overlay-shadow)]"
          @keydown="onMenuKeydown"
        >
          <button
            v-for="field in fields"
            :key="field.key"
            type="button"
            role="option"
            :data-search-field-option="field.key"
            :aria-selected="field.key === activeField.key"
            class="flex min-h-9 w-full items-center justify-between gap-2 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors sm:text-sm"
            :class="
              field.key === activeField.key
                ? 'bg-[var(--accent-soft)] font-semibold text-[var(--accent-text)]'
                : 'text-[var(--text-secondary)] hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)]'
            "
            @click="selectField(field.key)"
          >
            <span class="truncate">{{ field.label }}</span>
            <span
              v-if="values[field.key]?.trim()"
              class="h-1.5 w-1.5 shrink-0 rounded-full bg-[var(--accent)]"
              aria-hidden="true"
            />
          </button>
        </div>
      </Teleport>
    </template>

    <div class="relative flex h-full min-w-0 flex-1 items-center">
      <Search
        :size="15"
        aria-hidden="true"
        class="pointer-events-none ml-3 shrink-0 text-[var(--text-tertiary)]"
      />
      <input
        ref="inputRef"
        type="search"
        data-log-search-input
        :name="activeField.inputName || inputName"
        :value="activeValue"
        :maxlength="activeField.maxlength"
        :placeholder="activeField.placeholder"
        :aria-label="activeField.placeholder || activeField.label"
        class="h-full w-full min-w-0 border-0 bg-transparent pl-2 pr-8 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] focus:outline-hidden"
        @input="onInput"
      />
      <button
        v-if="activeValue"
        type="button"
        :aria-label="t('logs.clearFilterValue', { label: activeField.label })"
        class="absolute right-1.5 inline-flex h-7 w-7 items-center justify-center rounded-md text-[var(--text-tertiary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-ring"
        @click="clearActive"
      >
        <X :size="14" aria-hidden="true" />
      </button>
    </div>
  </div>
</template>

<style scoped>
html.dark .log-unified-search {
  background: var(--studio-control-track);
  border-color: var(--outline);
  box-shadow: inset 0 1px 2px rgba(0, 0, 0, 0.2);
}

html.dark .log-unified-search:focus-within {
  border-color: var(--accent);
  box-shadow: var(--focus-halo);
}

.log-unified-search input[type='search'],
.log-unified-search input[type='search']:focus,
.log-unified-search input[type='search']:focus-visible {
  border: 0;
  outline: none;
  box-shadow: none;
}

.log-unified-search input[type='search']::-webkit-search-cancel-button {
  -webkit-appearance: none;
  appearance: none;
}
</style>
