<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import { useServerText } from '@/composables/useServerText'
const model = defineModel<Record<string, Record<string, boolean>>>({
  required: true,
})
const { t } = useI18n(),
  resources = ref<
    Array<{
      resource: string
      label_key: string
      actions: Array<{
        action: string
        label_key: string
        description_key: string
      }>
    }>
  >([]),
  error = ref('')
// Catalog labels and descriptions are backend English text; zh-CN reads them
// from server-messages/permissions.json.
const serverText = useServerText()
onMounted(async () => {
  try {
    resources.value = (
      await api.get<{ resources: typeof resources.value }>('/api/authz/catalog')
    ).resources
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  }
})
function change(resource: string, action: string, enabled: boolean) {
  model.value = {
    ...model.value,
    [resource]: { ...model.value[resource], [action]: enabled },
  }
}
</script>
<template>
  <fieldset class="min-w-0 space-y-3 text-left">
    <legend class="mb-2 text-sm font-semibold text-[var(--text-primary)]">
      {{ t('users.permissions') }}
    </legend>
    <ErrorBanner v-if="error" :message="error" />
    <fieldset
      v-for="resource in resources"
      :key="resource.resource"
      class="min-w-0 rounded-xl border border-[var(--border-subtle)] p-3"
    >
      <legend class="px-1 text-sm font-medium text-[var(--text-primary)]">
        {{ serverText(resource.label_key) }}
      </legend>
      <label
        v-for="action in resource.actions"
        :key="action.action"
        class="flex min-h-11 cursor-pointer items-start gap-3 py-2 text-sm text-[var(--text-primary)]"
        ><input
          type="checkbox"
          class="mt-0.5 h-4 w-4 shrink-0 accent-[var(--accent)] focus-ring"
          :checked="model[resource.resource]?.[action.action] === true"
          @change="
            change(
              resource.resource,
              action.action,
              ($event.target as HTMLInputElement).checked
            )
          "
        /><span class="min-w-0 break-words"
          >{{ serverText(action.label_key)
          }}<span
            v-if="action.description_key"
            class="block text-xs text-[var(--text-secondary)]"
            >{{ serverText(action.description_key) }}</span
          ></span
        ></label
      >
    </fieldset>
  </fieldset>
</template>
