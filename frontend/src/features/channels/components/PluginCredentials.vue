<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import {
  getPluginCredentials,
  type PluginCredentialSummary,
} from '../lib/plugin-login'
import PluginLoginDialog from './PluginLoginDialog.vue'
const props = defineProps<{ channelId: number; canWrite: boolean }>()
const { t } = useI18n()
const credentials = ref<PluginCredentialSummary[]>([])
const error = ref('')
const login = ref<{ keyIndex: number | null; add: boolean } | null>(null)
let request: AbortController | undefined
async function load() {
  request?.abort()
  request = new AbortController()
  const active = request
  error.value = ''
  try {
    const result = await getPluginCredentials(props.channelId, active.signal)
    if (!active.signal.aborted) credentials.value = result
  } catch (cause) {
    if (!active.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
watch(
  () => props.channelId,
  () => {
    login.value = null
    void load()
  },
  { immediate: true }
)
onBeforeUnmount(() => request?.abort())
</script>
<template>
  <div class="sibling-space-y-2">
    <p v-if="error" role="alert" class="text-sm text-[var(--status-danger)]">
      {{ error }}
    </p>
    <div
      v-for="credential in credentials"
      :key="credential.index"
      class="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-[var(--border-subtle)] p-2"
    >
      <span class="break-all text-sm">{{
        credential.label ||
        credential.account_id ||
        `${t('pluginExtensions.credential')} ${credential.index + 1}`
      }}</span
      ><ConsoleButton
        v-if="canWrite"
        size="sm"
        variant="secondary"
        @click="login = { keyIndex: credential.index, add: false }"
        >{{ t('pluginExtensions.relogin') }}</ConsoleButton
      >
    </div>
    <ConsoleButton
      v-if="canWrite"
      size="sm"
      @click="login = { keyIndex: null, add: credentials.length > 0 }"
      >{{
        t(
          credentials.length
            ? 'pluginExtensions.addAccount'
            : 'pluginExtensions.signIn'
        )
      }}</ConsoleButton
    ><PluginLoginDialog
      v-if="login"
      :channel-id="channelId"
      :key-index="login.keyIndex"
      :add="login.add"
      @close="login = null"
      @done="load"
    />
  </div>
</template>
