<script setup lang="ts">
// Upstream components/marketplace-sources-dialog.tsx. The dialog edits a
// local copy so a half-typed row never reaches the server.
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Trash2 } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import IconButton from '@/components/common/IconButton.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import TextInput from '@/components/common/TextInput.vue'
import { useToast } from '@/composables/useToast'

import { updateMarketplaceSources } from '../api'
import { isDefaultMarketplaceSource } from '../lib/marketplace'
import type { MarketplaceSource } from '../types'
import PluginAlert from './PluginAlert.vue'

const props = defineProps<{ sources: MarketplaceSource[] }>()
const emit = defineEmits<{ close: []; saved: [sources: MarketplaceSource[]] }>()
const { t } = useI18n()
const toast = useToast()

// Client-only row identity keeps input state attached to the right row.
let nextRowId = 0
const draft = ref(
  props.sources.map((source) => ({ ...source, rowId: ++nextRowId }))
)
const saving = ref(false)
function isHttpsUrl(value: string) {
  try {
    const url = new URL(value.trim())
    return url.protocol === 'https:' && !url.username && !url.password
  } catch {
    return false
  }
}
const invalidRow = computed(() =>
  draft.value.some((row) => !row.name.trim() || !isHttpsUrl(row.index_url))
)

async function save() {
  saving.value = true
  try {
    const saved = await updateMarketplaceSources(
      draft.value.map((row) => ({
        name: row.name.trim(),
        index_url: row.index_url.trim(),
      }))
    )
    toast.success(t('taskPlugins.sourcesUpdated'))
    emit('saved', saved)
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ConsoleModal
    :open="true"
    :title="t('taskPlugins.marketplaceSources')"
    :subtitle="t('taskPlugins.sourcesDesc')"
    size="lg"
    :close-disabled="saving"
    @close="emit('close')"
  >
    <div class="space-y-3">
      <p v-if="!draft.length" class="text-sm text-[var(--text-tertiary)]">
        {{ t('taskPlugins.noSources') }}
      </p>
      <p
        v-if="invalidRow"
        role="alert"
        class="text-sm text-[var(--status-danger-text)]"
      >
        {{ t('taskPlugins.httpsSourceRequired') }}
      </p>
      <div
        v-for="(row, index) in draft"
        :key="row.rowId"
        class="space-y-2 rounded-xl border border-[var(--border-subtle)] p-3"
      >
        <div class="flex items-center justify-between gap-2">
          <label
            :for="`marketplace-source-name-${row.rowId}`"
            class="text-sm font-medium"
            >{{ t('taskPlugins.sourceName') }}</label
          >
          <div class="flex items-center gap-2">
            <StatusChip
              v-if="isDefaultMarketplaceSource(row.index_url)"
              tone="neutral"
              >{{ t('taskPlugins.official') }}</StatusChip
            >
            <StatusChip v-else tone="danger">{{
              t('taskPlugins.thirdPartyRisk')
            }}</StatusChip>
            <IconButton
              tone="danger"
              :label="
                t('taskPlugins.removeSource', {
                  name: row.name || row.index_url,
                })
              "
              @click="draft.splice(index, 1)"
            >
              <Trash2 :size="15" aria-hidden="true" />
            </IconButton>
          </div>
        </div>
        <TextInput
          :id="`marketplace-source-name-${row.rowId}`"
          v-model="row.name"
        />
        <label
          :for="`marketplace-source-url-${row.rowId}`"
          class="block text-sm font-medium"
          >{{ t('taskPlugins.indexUrl') }}</label
        >
        <TextInput
          :id="`marketplace-source-url-${row.rowId}`"
          v-model="row.index_url"
          type="url"
          inputmode="url"
          placeholder="https://example.com/index.json"
        />
      </div>
      <ConsoleButton
        variant="secondary"
        @click="draft.push({ name: '', index_url: '', rowId: ++nextRowId })"
      >
        <Plus :size="15" aria-hidden="true" />
        {{ t('taskPlugins.addSource') }}
      </ConsoleButton>
      <PluginAlert :title="t('taskPlugins.sourceRiskTitle')">
        {{ t('taskPlugins.sourceRiskDesc') }}
      </PluginAlert>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton
          variant="secondary"
          :disabled="saving"
          @click="emit('close')"
        >
          {{ t('common.cancel') }}
        </ConsoleButton>
        <ConsoleButton :disabled="invalidRow" :loading="saving" @click="save">
          {{ saving ? t('taskPlugins.saving') : t('taskPlugins.save') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
