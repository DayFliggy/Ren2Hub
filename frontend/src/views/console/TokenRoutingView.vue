<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  tokenRoutingApi,
  type TokenRoute,
  type TokenRoutePreview,
  type TokenRouteChannel,
} from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import { tokenRoutingMessages } from '@/i18n/tokenRouting'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'

const { t } = useI18n({ useScope: 'local', messages: tokenRoutingMessages })
const route = useRoute()
const toast = useToast()
const tokenId = computed(() => Number(route.params.id))
const data = ref<TokenRoute | null>(null)
const channels = ref<TokenRouteChannel[]>([])
const selected = ref('')
const loading = ref(false)
const saving = ref(false)
const conflict = ref(false)
const error = ref('')
const request = useLatestRequest()
const saveRequest = useLatestRequest(),
  previewRequest = useLatestRequest(),
  auth = useAuthStore()
const canWrite = computed(() => auth.hasPermission('routing', 'write'))
const previewModel = ref(''),
  previewPath = ref('/v1/chat/completions'),
  preview = ref<TokenRoutePreview | null>(null),
  previewLoading = ref(false)
async function loadPreview() {
  if (!previewModel.value.trim()) return
  previewLoading.value = true
  error.value = ''
  const result = await previewRequest.run((signal) =>
    tokenRoutingApi.preview(
      tokenId.value,
      previewModel.value.trim(),
      previewPath.value,
      signal
    )
  )
  if (result.stale) return
  previewLoading.value = false
  if (result.ok) preview.value = result.value
  else
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
}
watch([tokenId, previewModel, previewPath], () => {
  previewRequest.cancel()
  preview.value = null
  previewLoading.value = false
})
const available = computed(() =>
  channels.value.filter(
    (channel) =>
      !data.value?.entries.some((entry) => entry.channel_id === channel.id)
  )
)
async function load() {
  loading.value = true
  error.value = ''
  const result = await request.run((signal) =>
    Promise.all([
      tokenRoutingApi.get(tokenId.value, signal),
      tokenRoutingApi.channels(tokenId.value, signal),
    ])
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  ;[data.value, channels.value] = result.value
  conflict.value = false
}
function add() {
  const id = Number(selected.value)
  if (!data.value || !available.value.some((channel) => channel.id === id))
    return
  data.value.entries.push({
    channel_id: id,
    enabled: true,
    position: data.value.entries.length,
  })
  selected.value = ''
}
function move(index: number, offset: number) {
  const entries = data.value?.entries
  if (!entries || index + offset < 0 || index + offset >= entries.length) return
  const [entry] = entries.splice(index, 1)
  if (entry) entries.splice(index + offset, 0, entry)
}
async function save() {
  if (!data.value || saving.value || conflict.value || !canWrite.value) return
  saving.value = true
  const result = await saveRequest.run(() =>
    tokenRoutingApi.save(tokenId.value, data.value!)
  )
  if (result.stale) return
  saving.value = false
  if (result.ok) {
    data.value = result.value
    preview.value = null
    toast.success(t('routing.saved'))
  } else {
    conflict.value =
      result.error instanceof ApiError && result.error.status === 409
    error.value = conflict.value
      ? t('routing.conflict')
      : result.error instanceof Error
        ? result.error.message
        : String(result.error)
  }
}
watch(
  () => auth.hasPermission('routing', 'write'),
  () => {
    saveRequest.cancel()
    saving.value = false
  }
)
watch(
  tokenId,
  () => {
    saveRequest.cancel()
    saving.value = false
    data.value = null
    void load()
  },
  { immediate: true }
)
</script>
<template>
  <div>
    <PageBreadcrumb :crumbs="[t('routing.title')]" />
    <ConsoleCard class="p-5 sm:p-7">
      <h1 class="text-xl font-semibold">{{ t('routing.title') }}</h1>
      <p class="mt-2 text-sm text-[var(--text-secondary)]">
        {{ t('routing.description') }}
      </p>
      <p
        v-if="error"
        role="alert"
        class="mt-4 text-[var(--status-danger-text)]"
      >
        {{ error }}
      </p>
      <div class="mt-4 flex flex-wrap gap-3">
        <ConsoleButton :disabled="loading || saving" @click="load">{{
          t('routing.reload')
        }}</ConsoleButton>
        <ConsoleButton
          :disabled="!data || loading || saving || conflict || !canWrite"
          @click="save"
          >{{ t('routing.save') }}</ConsoleButton
        >
      </div>
      <template v-if="data">
        <fieldset
          class="mt-6 flex flex-wrap gap-5"
          :disabled="saving || loading || !canWrite"
        >
          <label class="flex items-center gap-2"
            ><input v-model="data.mode" type="radio" value="auto" />{{
              t('routing.automatic')
            }}</label
          >
          <label class="flex items-center gap-2"
            ><input v-model="data.mode" type="radio" value="manual" />{{
              t('routing.manual')
            }}</label
          >
        </fieldset>
        <p
          v-if="data.mode === 'auto'"
          class="mt-4 text-sm text-[var(--text-secondary)]"
        >
          {{ t('routing.autoHint') }}
        </p>
        <template v-else>
          <div class="mt-5 flex flex-wrap gap-3">
            <select
              v-model="selected"
              :aria-label="t('routing.select')"
              class="max-w-full rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-2"
            >
              <option value="">{{ t('routing.select') }}</option>
              <option
                v-for="channel in available"
                :key="channel.id"
                :value="channel.id"
              >
                {{ channel.name }} (#{{ channel.id }})
              </option>
            </select>
            <ConsoleButton
              :disabled="!selected || saving || !canWrite"
              @click="add"
              >{{ t('routing.add') }}</ConsoleButton
            >
          </div>
          <p
            v-if="!channels.length"
            class="mt-3 text-sm text-[var(--text-secondary)]"
          >
            {{ t('routing.noChannels') }}
          </p>
          <p v-if="!data.entries.length" class="mt-5 text-sm">
            {{ t('routing.empty') }}
          </p>
          <ol
            class="mt-5 sibling-divide-y sibling-divide-[var(--border-subtle)]"
          >
            <li
              v-for="(entry, index) in data.entries"
              :key="entry.channel_id"
              class="flex flex-wrap items-center justify-between gap-3 py-4"
            >
              <span class="min-w-0 break-words"
                >{{ index + 1 }}.
                {{
                  channels.find((channel) => channel.id === entry.channel_id)
                    ?.name ?? t('routing.unavailable', { id: entry.channel_id })
                }}</span
              >
              <div class="flex flex-wrap items-center gap-2">
                <ConsoleToggle
                  v-model="entry.enabled"
                  :label="t('routing.enabled')"
                  :disabled="saving || !canWrite"
                />
                <ConsoleButton
                  :disabled="index === 0 || saving || !canWrite"
                  @click="move(index, -1)"
                  >{{ t('routing.up') }}</ConsoleButton
                >
                <ConsoleButton
                  :disabled="
                    index === data.entries.length - 1 || saving || !canWrite
                  "
                  @click="move(index, 1)"
                  >{{ t('routing.down') }}</ConsoleButton
                >
                <ConsoleButton
                  :disabled="saving || !canWrite"
                  @click="data.entries.splice(index, 1)"
                  >{{ t('routing.remove') }}</ConsoleButton
                >
              </div>
            </li>
          </ol>
        </template>
      </template>
      <section
        v-if="data"
        class="mt-7 border-t border-[var(--border-subtle)] pt-5"
      >
        <h2 class="font-semibold">{{ t('routing.preview') }}</h2>
        <p class="mt-2 text-sm text-[var(--text-secondary)]">
          {{ t('routing.previewHint') }}
        </p>
        <div class="mt-4 flex flex-wrap gap-3">
          <input
            v-model="previewModel"
            class="console-input min-w-0"
            :aria-label="t('routing.model')"
            :placeholder="t('routing.model')"
          /><select
            v-model="previewPath"
            class="console-input"
            :aria-label="t('routing.path')"
          >
            <option
              v-for="path in [
                '/v1/chat/completions',
                '/v1/responses',
                '/v1/messages',
                '/v1/embeddings',
                '/v1/images/generations',
                '/v1/audio/transcriptions',
              ]"
              :key="path"
              :value="path"
            >
              {{ path }}
            </option></select
          ><ConsoleButton
            :disabled="!previewModel.trim() || previewLoading || saving"
            @click="loadPreview"
            >{{ t('routing.preview') }}</ConsoleButton
          >
        </div>
        <div v-if="preview" class="mt-4">
          <p class="text-sm">
            {{ t('routing.previewVersion', { version: preview.version }) }} ·
            {{ preview.groups.join(', ') }}
          </p>
          <p v-if="!preview.entries.length" class="mt-2">
            {{ t('routing.empty') }}
          </p>
          <ul class="mt-2 space-y-2">
            <li
              v-for="item in preview.entries"
              :key="`${item.group}:${item.channel_id}`"
            >
              {{ item.name || `#${item.channel_id}` }} ·
              {{ t(`routing.reasons.${item.reason}`)
              }}<span v-if="item.group"> · {{ item.group }}</span>
            </li>
          </ul>
        </div>
      </section>
    </ConsoleCard>
  </div>
</template>
