<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import FormField from '@/components/common/FormField.vue'
import {
  parseAdvancedChannelConfig,
  parseChannelObject,
  type AdvancedChannelConfig,
  type AdvancedChannelRoute,
} from '@/utils/channelSettings'

const model = defineModel<string>({ required: true })
defineProps<{ disabled?: boolean }>()
const { t } = useI18n()
const mode = ref<'visual' | 'json'>('visual')
const config = computed(() => {
  try {
    const parsed = parseChannelObject(model.value)
    if (
      !Array.isArray(parsed.advanced_routes) ||
      parsed.advanced_routes.some(
        (row) =>
          !row ||
          typeof row !== 'object' ||
          Array.isArray(row) ||
          (row.models !== undefined && !Array.isArray(row.models))
      )
    )
      return null
    return parsed as AdvancedChannelConfig
  } catch {
    return null
  }
})
const valid = computed(() => {
  try {
    parseAdvancedChannelConfig(model.value)
    return true
  } catch {
    return false
  }
})
const routes = computed(() => config.value?.advanced_routes ?? [])
const converters = [
  'none',
  'anthropic_messages_to_openai_chat_completions',
  'openai_chat_completions_to_anthropic_messages',
  'openai_chat_completions_to_openai_responses',
  'openai_responses_to_openai_chat_completions',
  'openai_responses_to_gemini_generate_content',
  'gemini_generate_content_to_openai_chat_completions',
  'openai_chat_completions_to_gemini_generate_content',
  'jina_rerank_to_sglang',
  'claude_messages_to_openai_responses',
  'gemini_generate_content_to_openai_responses',
]

function update(index: number, patch: Partial<AdvancedChannelRoute>) {
  if (!config.value) return
  const next = structuredClone(config.value)
  const current = next.advanced_routes[index]
  if (!current) return
  next.advanced_routes[index] = { ...current, ...patch }
  model.value = JSON.stringify(next, null, 2)
}
function input(event: Event) {
  return (event.target as HTMLInputElement).value
}
function setAuth(index: number, field: string, value: string) {
  update(index, {
    auth: {
      type: 'header',
      name: 'Authorization',
      value: 'Bearer {api_key}',
      ...routes.value[index]?.auth,
      [field]: value,
    },
  })
}
function add() {
  if (!config.value) return
  const next = structuredClone(config.value)
  next.advanced_routes.push({
    incoming_path: '/v1/chat/completions',
    upstream_path: '/v1/chat/completions',
    converter: 'none',
    auth: { type: 'header', name: 'Authorization', value: 'Bearer {api_key}' },
  })
  model.value = JSON.stringify(next, null, 2)
}
function remove(index: number) {
  if (!config.value) return
  const next = structuredClone(config.value)
  next.advanced_routes.splice(index, 1)
  model.value = JSON.stringify(next, null, 2)
}
</script>

<template>
  <div class="min-w-0 sibling-space-y-3">
    <div class="flex flex-wrap gap-2">
      <ConsoleButton
        variant="secondary"
        :aria-pressed="mode === 'visual'"
        @click="mode = 'visual'"
        >{{ t('channels.modelMappingVisual') }}</ConsoleButton
      >
      <ConsoleButton
        variant="secondary"
        :aria-pressed="mode === 'json'"
        @click="mode = 'json'"
        >{{ t('channels.modelMappingJson') }}</ConsoleButton
      >
    </div>
    <textarea
      v-if="mode === 'json'"
      v-model="model"
      :disabled="disabled"
      :aria-label="t('channels.advancedRouteJson')"
      rows="10"
      class="route-input w-full font-mono text-xs"
    />
    <template v-else>
      <article
        v-for="(route, index) in routes"
        :key="index"
        class="sibling-space-y-3 rounded-xl border border-[var(--border-subtle)] p-3"
      >
        <div class="grid min-w-0 gap-3 sm:grid-cols-2">
          <FormField :label="t('channels.incomingPath')"
            ><input
              class="route-input"
              :value="route.incoming_path"
              :disabled="disabled"
              @change="update(index, { incoming_path: input($event) })"
          /></FormField>
          <FormField :label="t('channels.upstreamPath')"
            ><input
              class="route-input"
              :value="route.upstream_path"
              :disabled="disabled"
              @change="update(index, { upstream_path: input($event) })"
          /></FormField>
          <FormField :label="t('channels.routeConverter')"
            ><select
              class="route-input"
              :value="route.converter || 'none'"
              :disabled="disabled"
              @change="
                update(index, {
                  converter: input($event),
                  pass_through_body_enabled: false,
                })
              "
            >
              <option v-for="value in converters" :key="value" :value="value">
                {{ value }}
              </option>
            </select></FormField
          >
          <FormField
            :label="t('channels.routeModels')"
            :hint="t('channels.routeModelsHint')"
            ><input
              class="route-input"
              :value="route.models?.join(',') ?? ''"
              :disabled="disabled"
              @change="
                update(index, {
                  models: input($event)
                    .split(',')
                    .map((x) => x.trim())
                    .filter(Boolean),
                })
              "
          /></FormField>
          <FormField :label="t('channels.routeAuth')"
            ><select
              class="route-input"
              :value="route.auth?.type ?? 'header'"
              :disabled="disabled"
              @change="setAuth(index, 'type', input($event))"
            >
              <option value="header">Header</option>
              <option value="query">Query</option>
              <option value="none">{{ t('channels.authNone') }}</option>
            </select></FormField
          >
          <FormField
            v-if="route.auth?.type !== 'none'"
            :label="t('channels.authName')"
            ><input
              class="route-input"
              :value="route.auth?.name ?? 'Authorization'"
              :disabled="disabled"
              @change="setAuth(index, 'name', input($event))"
          /></FormField>
          <FormField
            v-if="route.auth?.type !== 'none'"
            :label="t('channels.authValue')"
            :hint="t('channels.authValueHint')"
            class="sm:col-span-2"
            ><input
              class="route-input"
              :value="route.auth?.value ?? 'Bearer {api_key}'"
              :disabled="disabled"
              @change="setAuth(index, 'value', input($event))"
          /></FormField>
        </div>
        <label class="flex items-center gap-2 text-sm"
          ><input
            type="checkbox"
            :checked="route.pass_through_body_enabled"
            :disabled="
              disabled ||
              !['none', '', 'jina_rerank_to_sglang'].includes(
                route.converter
              ) ||
              route.incoming_path === '/v1/models' ||
              route.incoming_path === '/v1/dashboard/billing/credit_grants'
            "
            @change="
              update(index, {
                pass_through_body_enabled: ($event.target as HTMLInputElement)
                  .checked,
              })
            "
          />{{ t('channels.routePassthrough') }}</label
        >
        <ConsoleButton
          variant="ghost"
          :disabled="disabled"
          @click="remove(index)"
          >{{ t('channels.removeRoute') }}</ConsoleButton
        >
      </article>
      <ConsoleButton
        variant="secondary"
        :disabled="disabled || !config"
        @click="add"
        >{{ t('channels.addRoute') }}</ConsoleButton
      >
    </template>
    <p v-if="!valid" role="alert" class="text-sm text-[var(--status-danger)]">
      {{ t('channels.invalidRouteConfig') }}
    </p>
  </div>
</template>

<style scoped>
.route-input {
  width: 100%;
  min-width: 0;
  padding: 0.625rem;
  border: 1px solid var(--border-subtle);
  border-radius: 0.5rem;
  background: var(--surface-solid);
  color: var(--text-primary);
  font-size: 0.8125rem;
}
</style>
