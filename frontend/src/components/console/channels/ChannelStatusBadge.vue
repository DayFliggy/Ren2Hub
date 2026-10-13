<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusChip from '@/components/common/StatusChip.vue'
import {
  adminChannelStatusLabelKey,
  adminChannelStatusTone,
  isRetiredChannelType,
} from '@/constants/adminChannels'
import { isLocaleCode } from '@/i18n'
import { translateServerText } from '@/i18n/serverMessages'
import type { AdminChannel } from '@/types/console'

const props = defineProps<{ channel: AdminChannel; mobile?: boolean }>()
const { t, locale } = useI18n()
// Reasons the gateway writes are fixed English sentences; provider text passes
// through unchanged.
const reasonText = computed(() => {
  const reason = props.channel.status_reason ?? ''
  return isLocaleCode(locale.value)
    ? translateServerText(locale.value, reason)
    : reason
})
const anchor = ref<HTMLButtonElement | null>(null)
const shown = ref(false)
const position = ref({ left: '0px', top: '0px' })
const id = useId()
const timestamp = computed(() =>
  props.channel.status_time
    ? new Date(props.channel.status_time * 1000).toLocaleString(locale.value)
    : ''
)

function hide() {
  shown.value = false
}
function scrollAway(event: Event) {
  if (
    event.target instanceof Element &&
    event.target.closest('.channel-status-reason')
  )
    return
  hide()
}
function show() {
  const rect = anchor.value?.getBoundingClientRect()
  if (!rect) return
  const width = Math.min(320, window.innerWidth - 32)
  const below = window.innerHeight - rect.bottom > 180
  position.value = {
    left: `${Math.max(16, Math.min(rect.left, window.innerWidth - width - 16))}px`,
    top: `${below ? rect.bottom + 8 : Math.max(16, rect.top - Math.min(320, window.innerHeight * 0.45) - 8)}px`,
  }
  shown.value = true
}
watch(
  () => [props.channel.id, props.channel.status, props.channel.status_reason],
  hide
)
watch(shown, (visible) => {
  window.removeEventListener('scroll', scrollAway, true)
  window.removeEventListener('resize', hide)
  if (visible) {
    window.addEventListener('scroll', scrollAway, true)
    window.addEventListener('resize', hide)
  }
})
onBeforeUnmount(() => {
  window.removeEventListener('scroll', scrollAway, true)
  window.removeEventListener('resize', hide)
})
</script>

<template>
  <StatusChip
    v-if="isRetiredChannelType(channel.type)"
    tone="warning"
    :title="t('channels.retiredHint')"
    >{{ t('channels.statusRetired') }}</StatusChip
  >
  <span v-else-if="channel.status === 3" class="inline-flex min-w-0">
    <button
      ref="anchor"
      type="button"
      class="rounded-sm focus-ring"
      :aria-label="t('channels.disabledReason')"
      :aria-expanded="shown"
      :aria-describedby="shown ? id : undefined"
      @mouseenter="!mobile && show()"
      @mouseleave="!mobile && hide()"
      @focus="!mobile && show()"
      @blur="hide"
      @keydown.esc.stop.prevent="hide"
      @click.stop="mobile && shown ? hide() : show()"
    >
      <StatusChip :tone="adminChannelStatusTone(channel.status)">{{
        t(adminChannelStatusLabelKey(channel.status))
      }}</StatusChip>
    </button>
    <Teleport to="body">
      <div
        v-if="shown"
        :id="id"
        role="tooltip"
        class="channel-status-reason"
        :style="position"
      >
        <p class="font-semibold">{{ t('channels.disabledReason') }}</p>
        <p class="mt-2 whitespace-pre-wrap">
          {{ reasonText || t('channels.disabledReasonMissing') }}
        </p>
        <p v-if="timestamp" class="mt-2 text-xs text-[var(--text-secondary)]">
          {{ t('channels.disabledAt', { time: timestamp }) }}
        </p>
      </div>
    </Teleport>
  </span>
  <StatusChip v-else :tone="adminChannelStatusTone(channel.status)">{{
    t(adminChannelStatusLabelKey(channel.status))
  }}</StatusChip>
</template>

<style scoped>
.channel-status-reason {
  position: fixed;
  z-index: 80;
  width: min(20rem, calc(100vw - 2rem));
  max-height: min(20rem, 45vh);
  overflow: auto;
  overflow-wrap: anywhere;
  border: 1px solid var(--border-default);
  border-radius: var(--shape-overlay);
  padding: 0.75rem;
  background: var(--surface-overlay);
  color: var(--text-primary);
  box-shadow: var(--overlay-shadow);
  font-size: 0.8125rem;
}
</style>
