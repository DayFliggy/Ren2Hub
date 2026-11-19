<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { listRequestPluginInstances } from '@/features/task-plugins/api'
import type { RequestPluginInstance } from '@/features/task-plugins/types'
const props = defineProps<{ disabled?: boolean }>()
const model = defineModel<string[]>({ required: true })
const { t } = useI18n()
const instances = ref<RequestPluginInstance[]>([])
const error = ref('')
const loading = ref(true)
const selected = ref('')
const request = new AbortController()
async function load() {
  try {
    const result = await listRequestPluginInstances(request.signal)
    if (!request.signal.aborted) instances.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    loading.value = false
  }
}
// Global instances and those that run where their plugin's scope says are not
// placed on a channel: the run plan never runs them for a channel binding.
function bindable(item: RequestPluginInstance) {
  return !model.value.includes(item.id) && !item.global && !item.plugin_scope
}
function followsPlugin(id: string) {
  return !!instances.value.find((item) => item.id === id)?.plugin_scope
}
function add() {
  if (selected.value && !model.value.includes(selected.value))
    model.value = [...model.value, selected.value]
  selected.value = ''
}
function move(index: number, delta: number) {
  const values = [...model.value]
  const value = values.splice(index, 1)[0]
  if (value) values.splice(index + delta, 0, value)
  model.value = values
}
onBeforeUnmount(() => request.abort())
void load()
</script>
<template>
  <fieldset
    :disabled="props.disabled || loading || !!error"
    class="sibling-space-y-2"
  >
    <legend class="mb-2 text-sm font-medium">
      {{ t('pluginExtensions.bindings') }}
    </legend>
    <p v-if="error" role="alert" class="text-sm text-[var(--status-danger)]">
      {{ error }}
    </p>
    <p class="text-xs text-[var(--text-secondary)]">
      {{ t('pluginExtensions.bindingHint') }}
    </p>
    <div
      v-for="(id, index) in model"
      :key="id"
      class="flex flex-wrap items-center gap-2"
    >
      <span class="min-w-0 flex-1 break-all text-sm"
        >{{
          instances.find((item) => item.id === id)?.name ||
          `${id} · ${t('pluginExtensions.unavailable')}`
        }}<span
          v-if="followsPlugin(id)"
          data-binding-follows-plugin
          class="block text-xs text-[var(--text-tertiary)]"
          >{{ t('pluginExtensions.bindingFollowsPlugin') }}</span
        ></span
      ><ConsoleButton
        size="sm"
        variant="secondary"
        :disabled="index === 0"
        @click="move(index, -1)"
        >{{ t('pluginExtensions.up') }}</ConsoleButton
      ><ConsoleButton
        size="sm"
        variant="secondary"
        :disabled="index === model.length - 1"
        @click="move(index, 1)"
        >{{ t('pluginExtensions.down') }}</ConsoleButton
      ><ConsoleButton
        size="sm"
        variant="secondary"
        @click="model = model.filter((value) => value !== id)"
        >{{ t('pluginExtensions.remove') }}</ConsoleButton
      >
    </div>
    <div class="flex gap-2">
      <select
        v-model="selected"
        :aria-label="t('pluginExtensions.bindings')"
        class="min-w-0 flex-1 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-2"
      >
        <option value="" />
        <option
          v-for="item in instances.filter(bindable)"
          :key="item.id"
          :value="item.id"
        >
          {{ item.name }}
        </option></select
      ><ConsoleButton size="sm" :disabled="!selected" @click="add">{{
        t('pluginExtensions.add')
      }}</ConsoleButton>
    </div>
  </fieldset>
</template>
