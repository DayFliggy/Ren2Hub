<script setup lang="ts">
import {
  Eye,
  FileCode2,
  LoaderCircle,
  Pencil,
  Power,
  PowerOff,
  Route,
  Trash2,
} from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

import IconButton from '@/components/common/IconButton.vue'
import KeyIdentity from '@/components/console/keys/KeyIdentity.vue'
import KeyQuotaBar from '@/components/console/keys/KeyQuotaBar.vue'
import KeyRouteChip from '@/components/console/keys/KeyRouteChip.vue'
import { clientConfigMessages } from '@/i18n/clientConfig'
import type { TokenSummary } from '@/types/console'
import { tokenMeter } from '@/utils/tokenMeter'

const props = withDefaults(
  defineProps<{
    tokens: TokenSummary[]
    selectedIds: Array<string | number>
    allSelected: boolean
    toggleAllSelected: () => void
    toggleSelected: (token: TokenSummary) => void
    isToggling: (token: TokenSummary) => boolean
    toggleStatus: (token: TokenSummary) => void | Promise<void>
    viewKey: (token: TokenSummary) => void
    clientConfig?: (token: TokenSummary) => void
    editKey: (token: TokenSummary) => void
    configureRouting?: (token: TokenSummary) => void
    deleteKey: (token: TokenSummary) => void
    routingEnabled?: boolean
    now: number
  }>(),
  {
    routingEnabled: false,
    clientConfig: undefined,
    configureRouting: undefined,
  }
)

const { t } = useI18n()
const { t: configT } = useI18n({
  useScope: 'local',
  messages: clientConfigMessages,
})

// Only keys that cannot serve fade back; a spent key stays at full strength so
// its red quota bar keeps reading as something to act on.
function isBlocked(token: TokenSummary): boolean {
  const state = tokenMeter(token, props.now).state
  return state === 'disabled' || state === 'expired'
}
</script>

<template>
  <div>
    <label
      class="flex items-center gap-2 border-b border-[var(--border-subtle)] px-4 py-2 text-xs text-[var(--text-secondary)]"
    >
      <input
        type="checkbox"
        class="checkbox-round"
        :checked="allSelected"
        :aria-label="t('common.selectAll')"
        @change="toggleAllSelected"
      />
      {{ t('common.selectAll') }}
    </label>

    <div class="sibling-divide-y sibling-divide-[var(--border-subtle)]">
      <article
        v-for="token in tokens"
        :key="token.id"
        data-key-mobile-row
        class="no-handdrawn min-w-0 px-4 py-4 transition-colors"
        :class="isBlocked(token) ? 'bg-[var(--surface-muted)] opacity-80' : ''"
      >
        <header class="flex min-w-0 items-start gap-2.5">
          <label class="mt-0.5 flex shrink-0 items-center justify-center">
            <input
              type="checkbox"
              class="checkbox-round"
              :checked="selectedIds.includes(token.id)"
              :aria-label="t('keys.selectKey', { name: token.name })"
              @change="toggleSelected(token)"
            />
          </label>
          <div class="min-w-0 flex-1">
            <KeyIdentity :token="token" :now="now" heading />
            <!-- Route labels run long, so they sit under the name, not beside it. -->
            <p class="mt-1.5 flex min-w-0" data-token-type>
              <KeyRouteChip
                :token="token"
                :interactive="routingEnabled && Boolean(configureRouting)"
                @configure="configureRouting?.(token)"
              />
            </p>
          </div>
        </header>

        <KeyQuotaBar class="mt-3" :token="token" :now="now" />

        <!-- Key reveal stays paired with its masked preview; CRUD actions sit together. -->
        <footer
          class="mt-3 flex flex-col gap-2 border-t border-[var(--border-subtle)] pt-2.5 sm:flex-row sm:items-center sm:justify-between"
        >
          <div class="flex min-w-0 items-center gap-1.5 text-xs">
            <span class="shrink-0 text-[var(--text-tertiary)]">
              {{ t('keys.colKey') }}
            </span>
            <span
              class="min-w-0 truncate font-mono text-[0.6875rem] text-[var(--text-secondary)]"
              :title="token.key_preview"
            >
              {{ token.key_preview }}
            </span>
            <IconButton
              class="shrink-0"
              :label="t('keys.viewKey')"
              @click="viewKey(token)"
            >
              <Eye :size="16" aria-hidden="true" />
            </IconButton>
          </div>
          <div class="flex items-center justify-end gap-1">
            <IconButton
              v-if="routingEnabled && configureRouting"
              data-key-routing-action
              :label="t('keys.routing')"
              @click="configureRouting(token)"
            >
              <Route :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              v-if="clientConfig"
              :label="configT('title')"
              @click="clientConfig(token)"
            >
              <FileCode2 :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton :label="t('keys.editKey')" @click="editKey(token)">
              <Pencil :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              :label="t('keys.toggleKey')"
              :disabled="isToggling(token)"
              @click="toggleStatus(token)"
            >
              <LoaderCircle
                v-if="isToggling(token)"
                :size="16"
                class="animate-spin"
                aria-hidden="true"
              />
              <PowerOff
                v-else-if="token.status === 1"
                :size="16"
                aria-hidden="true"
              />
              <Power v-else :size="16" aria-hidden="true" />
            </IconButton>
            <IconButton
              :label="t('keys.deleteKey')"
              tone="danger"
              @click="deleteKey(token)"
            >
              <Trash2 :size="16" aria-hidden="true" />
            </IconButton>
          </div>
        </footer>
      </article>
    </div>
  </div>
</template>
