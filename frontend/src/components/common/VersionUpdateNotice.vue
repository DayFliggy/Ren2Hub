<script setup lang="ts">
// Shown once a status read reports a server version other than the one that
// served this page: the open page may call APIs the new release changed.
import { useI18n } from 'vue-i18n'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const app = useAppStore()

function reload() {
  window.location.reload()
}
</script>

<template>
  <!-- Outside #app like the toasts, so open dialogs do not make it inert. -->
  <Teleport to="body">
    <div
      v-if="app.serverVersionChanged"
      class="pencil-surface fixed inset-x-4 top-4 z-[var(--z-toast)] flex flex-wrap items-center gap-3 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-3.5 sm:left-1/2 sm:right-auto sm:w-[28rem] sm:-translate-x-1/2"
      data-handdrawn="toast"
      data-version-update
      role="status"
    >
      <span
        class="h-2.5 w-2.5 shrink-0 rounded-full bg-[var(--status-info)]"
        aria-hidden="true"
      />
      <p class="min-w-0 flex-1 text-sm text-[var(--text-primary)]">
        {{ t('common.versionUpdate.message') }}
      </p>
      <ConsoleButton size="sm" @click="reload">
        {{ t('common.versionUpdate.reload') }}
      </ConsoleButton>
    </div>
  </Teleport>
</template>
