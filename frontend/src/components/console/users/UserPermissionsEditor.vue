<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
const model = defineModel<Record<string, Record<string, boolean>>>({
  required: true,
})
const { t, te } = useI18n(),
  resources = ref<
    Array<{
      resource: string
      label_key: string
      actions: Array<{ action: string; label_key: string }>
    }>
  >([]),
  error = ref('')
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
  <fieldset class="space-y-3">
    <legend class="mb-2 font-semibold">{{ t('users.permissions') }}</legend>
    <ErrorBanner v-if="error" :message="error" />
    <div v-for="resource in resources" :key="resource.resource">
      <h4 class="text-sm">
        {{ te(resource.label_key) ? t(resource.label_key) : resource.resource }}
      </h4>
      <div class="flex flex-wrap gap-4">
        <label
          v-for="action in resource.actions"
          :key="action.action"
          class="inline-flex gap-2 text-sm"
          ><input
            type="checkbox"
            :checked="model[resource.resource]?.[action.action] === true"
            @change="
              change(
                resource.resource,
                action.action,
                ($event.target as HTMLInputElement).checked
              )
            "
          />{{
            te(action.label_key) ? t(action.label_key) : action.action
          }}</label
        >
      </div>
    </div>
  </fieldset>
</template>
