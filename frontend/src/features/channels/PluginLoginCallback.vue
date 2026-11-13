<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  PLUGIN_LOGIN_BROADCAST_CHANNEL,
  submitPluginLoginCallback,
} from './lib/plugin-login'
const { t } = useI18n()
const route = useRoute()
const error = ref('')
const pending = ref(true)
const request = new AbortController()
const plugin = String(route.params.key ?? '')
const params = Object.fromEntries(new URLSearchParams(window.location.search))
window.history.replaceState(window.history.state, '', window.location.pathname)
function broadcast(message: unknown) {
  if (typeof BroadcastChannel === 'undefined') return
  const channel = new BroadcastChannel(PLUGIN_LOGIN_BROADCAST_CHANNEL)
  channel.postMessage(message)
  channel.close()
}
async function finish() {
  if (!plugin || !params.state) {
    error.value = t('pluginExtensions.callbackFailed')
    pending.value = false
    return
  }
  try {
    const result = await submitPluginLoginCallback(
      plugin,
      params,
      request.signal
    )
    if (!request.signal.aborted) broadcast({ ...result, state: params.state })
  } catch (cause) {
    if (request.signal.aborted) return
    error.value = cause instanceof Error ? cause.message : String(cause)
    if (params.state) broadcast({ state: params.state, error: error.value })
  } finally {
    if (!request.signal.aborted) pending.value = false
  }
}
onBeforeUnmount(() => request.abort())
void finish()
</script>
<template>
  <section
    class="mx-auto max-w-lg rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-6 sibling-space-y-3"
  >
    <h1 class="font-medium">{{ t('pluginExtensions.login') }}</h1>
    <p v-if="pending">{{ t('pluginExtensions.pending') }}</p>
    <template v-else-if="error"
      ><p role="alert" class="text-[var(--status-danger)]">{{ error }}</p>
      <p>{{ t('pluginExtensions.callbackFailed') }}</p></template
    >
    <p v-else>{{ t('pluginExtensions.closeTab') }}</p>
  </section>
</template>
