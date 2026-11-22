<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  PLUGIN_LOGIN_BROADCAST_CHANNEL,
  submitPluginLoginCallback,
} from './lib/plugin-login'
import { errorMessage } from '@/api/clientError'
import {
  isPluginLoginCallbackSessionCurrent,
  takePluginLoginCallback,
} from './lib/plugin-login-callback'
const { t } = useI18n()
const route = useRoute()
const error = ref('')
const pending = ref(true)
const request = new AbortController()
const plugin = String(route.params.key ?? '')
const callback = takePluginLoginCallback(route.path)
const params = callback?.params ?? {}
const current = () =>
  !request.signal.aborted &&
  callback &&
  isPluginLoginCallbackSessionCurrent(callback.session)
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
    if (current()) broadcast({ ...result, state: params.state })
  } catch (cause) {
    if (!current()) return
    error.value = errorMessage(cause, t)
    if (params.state) broadcast({ state: params.state, error: error.value })
  } finally {
    if (!request.signal.aborted) {
      pending.value = false
      if (!current()) error.value = t('pluginExtensions.callbackFailed')
    }
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
