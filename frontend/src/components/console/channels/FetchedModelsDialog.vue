<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import TextInput from '@/components/common/TextInput.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import { resolveModelProvider } from '@/utils/modelProvider'

const props = defineProps<{ open: boolean; models: string[] }>()
const emit = defineEmits<{ close: []; apply: [models: string[]] }>()
const { t } = useI18n()
const search = ref('')
const selected = ref<string[]>([])
watch(
  () => props.open,
  (open) => {
    if (!open) return
    selected.value = [...new Set(props.models)]
    search.value = ''
  },
  { immediate: true }
)
const groups = computed(() => {
  const grouped = new Map<string, string[]>()
  const query = search.value.trim().toLowerCase()
  for (const model of new Set(props.models)) {
    const provider =
      resolveModelProvider(model)?.name ?? t('channels.otherProvider')
    if (
      query &&
      !model.toLowerCase().includes(query) &&
      !provider.toLowerCase().includes(query)
    )
      continue
    grouped.set(provider, [...(grouped.get(provider) ?? []), model])
  }
  return [...grouped].sort(([a], [b]) => a.localeCompare(b))
})
function toggle(models: string[], checked: boolean) {
  selected.value = checked
    ? [...new Set([...selected.value, ...models])]
    : selected.value.filter((model) => !models.includes(model))
}
</script>

<template>
  <ConsoleModal
    :open="open"
    :title="t('channels.selectFetchedModels')"
    size="lg"
    @close="emit('close')"
  >
    <TextInput
      v-model="search"
      :aria-label="t('channels.searchFetchedModels')"
      :placeholder="t('channels.searchFetchedModels')"
    />
    <p class="my-3 text-sm text-[var(--text-secondary)]">
      {{ t('channels.fetchedSelectionHint', { count: selected.length }) }}
    </p>
    <div class="space-y-4">
      <fieldset
        v-for="[provider, groupModels] in groups"
        :key="provider"
        class="rounded-lg border border-[var(--border-subtle)] p-3"
      >
        <legend class="px-1">
          <label class="flex items-center gap-2 text-sm font-medium">
            <input
              type="checkbox"
              :checked="groupModels.every((model) => selected.includes(model))"
              :indeterminate="
                groupModels.some((model) => selected.includes(model)) &&
                !groupModels.every((model) => selected.includes(model))
              "
              :aria-label="t('channels.selectProvider', { provider })"
              @change="
                toggle(groupModels, ($event.target as HTMLInputElement).checked)
              "
            />
            <VendorLogo :vendor="provider" :size="24" />{{ provider }} ({{
              groupModels.length
            }})
          </label>
        </legend>
        <div class="grid gap-2 sm:grid-cols-2">
          <label
            v-for="model in groupModels"
            :key="model"
            class="flex min-w-0 items-start gap-2 text-sm"
          >
            <input
              v-model="selected"
              type="checkbox"
              :value="model"
              class="mt-1 shrink-0"
            />
            <span class="min-w-0 break-all">{{ model }}</span>
          </label>
        </div>
      </fieldset>
      <p v-if="!groups.length" class="text-sm text-[var(--text-secondary)]">
        {{ t('channels.noFetchedModels') }}
      </p>
    </div>
    <template #footer>
      <div class="flex justify-end gap-2">
        <ConsoleButton variant="secondary" @click="emit('close')">{{
          t('common.cancel')
        }}</ConsoleButton>
        <ConsoleButton
          data-apply-fetched-models
          @click="emit('apply', selected)"
          >{{ t('channels.applyFetchedModels') }}</ConsoleButton
        >
      </div>
    </template>
  </ConsoleModal>
</template>
