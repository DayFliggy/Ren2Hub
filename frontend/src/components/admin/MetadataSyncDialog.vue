<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ArrowDownToLine, RefreshCw, X } from 'lucide-vue-next'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import {
  metadataApi,
  type SyncPreview,
  type SyncResult,
} from '@/api/adminManagement'
import { modelMetadataMessages } from '@/i18n/modelMetadata'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: modelMetadataMessages,
})
const language = ref(locale.value === 'zh-CN' ? 'zh-CN' : 'en')
const source = ref('official')
const preview = ref<SyncPreview | null>(null)
const result = ref<SyncResult | null>(null)
const selected = ref<string[]>([])
const saving = ref(false)
const loading = ref(false)
const error = ref('')
const search = ref('')
const page = ref(1)
const pageSize = ref(10)
const missingPage = ref(1)
const missingPageSize = ref(10)
let controller: AbortController | undefined
const conflicts = computed(() =>
  (preview.value?.conflicts ?? []).flatMap((model) =>
    model.fields.map((field) => ({
      ...field,
      model_name: model.model_name,
      key: JSON.stringify([model.model_name, field.field]),
    }))
  )
)
const filtered = computed(() =>
  conflicts.value.filter((field) =>
    `${field.model_name} ${field.field}`
      .toLocaleLowerCase()
      .includes(search.value.toLocaleLowerCase())
  )
)
const visible = computed(() =>
  filtered.value.slice(
    (page.value - 1) * pageSize.value,
    page.value * pageSize.value
  )
)
const visibleMissing = computed(() =>
  (preview.value?.missing ?? []).slice(
    (missingPage.value - 1) * missingPageSize.value,
    missingPage.value * missingPageSize.value
  )
)
const allVisibleSelected = computed(
  () =>
    visible.value.length > 0 &&
    visible.value.every((field) => selected.value.includes(field.key))
)
watch(search, () => {
  page.value = 1
})
watch(
  [source, language, () => props.open],
  (_, __, cleanup) => {
    controller?.abort()
    loading.value = false
    preview.value = null
    result.value = null
    selected.value = []
    search.value = ''
    page.value = 1
    missingPage.value = 1
    error.value = ''
    cleanup(() => controller?.abort())
  },
  { immediate: true }
)
function togglePage() {
  const keys = visible.value.map((field) => field.key)
  selected.value = allVisibleSelected.value
    ? selected.value.filter((key) => !keys.includes(key))
    : [...new Set([...selected.value, ...keys])]
}
async function load() {
  controller?.abort()
  const current = new AbortController()
  controller = current
  loading.value = true
  error.value = ''
  preview.value = null
  result.value = null
  try {
    const data = await metadataApi.preview(
      source.value,
      language.value,
      current.signal
    )
    if (current.signal.aborted) return
    preview.value = data
    selected.value = []
    page.value = 1
    missingPage.value = 1
  } catch (cause) {
    if (!current.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    if (!current.signal.aborted) loading.value = false
  }
}
async function synchronize() {
  if (!preview.value || saving.value) return
  saving.value = true
  error.value = ''
  try {
    const overwrite = preview.value.conflicts
      .map((conflict) => ({
        model_name: conflict.model_name,
        fields: conflict.fields
          .filter((field) =>
            selected.value.includes(
              JSON.stringify([conflict.model_name, field.field])
            )
          )
          .map((field) => field.field),
      }))
      .filter((item) => item.fields.length)
    result.value = await metadataApi.sync(
      source.value,
      language.value,
      overwrite
    )
    preview.value = null
    emit('saved')
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    saving.value = false
  }
}
</script>
<template>
  <ConsoleModal
    :open="open"
    :title="t('synchronize')"
    size="xl"
    :close-disabled="saving"
    @close="emit('close')"
  >
    <div class="admin-form">
      <div class="admin-fields">
        <label class="admin-field"
          ><span>{{ t('source') }}</span
          ><select v-model="source" :disabled="saving">
            <option value="official">{{ t('official') }}</option>
            <option value="config" disabled>{{ t('config') }}</option>
          </select></label
        >
        <label class="admin-field"
          ><span>{{ t('language') }}</span
          ><select v-model="language" :disabled="saving">
            <option value="zh-CN">中文</option>
            <option value="en">English</option>
            <option value="ja">日本語</option>
          </select></label
        >
      </div>
      <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
      <ConsoleButton
        variant="ghost"
        :loading="loading"
        :disabled="saving"
        @click="load"
        ><RefreshCw :size="16" />{{ t('preview') }}</ConsoleButton
      >
      <template v-if="preview">
        <section class="admin-section">
          <h3>{{ t('missingCount', { count: preview.missing.length }) }}</h3>
          <ul class="sync-missing">
            <li v-for="name in visibleMissing" :key="name">{{ name }}</li>
          </ul>
          <TablePagination
            v-if="preview.missing.length"
            v-model:page="missingPage"
            v-model:page-size="missingPageSize"
            :total="preview.missing.length"
          />
        </section>
        <section class="admin-section">
          <div class="sync-section-title">
            <h3>{{ t('conflicts') }}</h3>
            <span class="admin-muted">{{
              t('selectedFields', { count: selected.length })
            }}</span>
          </div>
          <label class="admin-field"
            ><span class="sr-only">{{ t('conflictSearch') }}</span
            ><input
              v-model="search"
              :placeholder="t('conflictSearch')"
              :disabled="saving"
          /></label>
          <label v-if="visible.length" class="admin-check"
            ><input
              type="checkbox"
              :checked="allVisibleSelected"
              :disabled="saving"
              @change="togglePage"
            />{{ t('selectConflicts') }}</label
          >
          <div class="admin-table-scroll">
            <table v-if="visible.length" class="admin-table sync-table">
              <thead>
                <tr>
                  <th>{{ t('model_name') }}</th>
                  <th>{{ t('field') }}</th>
                  <th>{{ t('local') }}</th>
                  <th>{{ t('upstream') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="field in visible" :key="field.key">
                  <td :data-label="t('model_name')">{{ field.model_name }}</td>
                  <td :data-label="t('field')">
                    <label class="admin-check"
                      ><input
                        v-model="selected"
                        type="checkbox"
                        :value="field.key"
                        :disabled="saving"
                        :aria-label="`${t('overwrite')} ${field.model_name} ${field.field}`"
                      />{{ t(field.field) }}</label
                    >
                  </td>
                  <td :data-label="t('local')">
                    <pre>{{ field.local }}</pre>
                  </td>
                  <td :data-label="t('upstream')">
                    <pre>{{ field.upstream }}</pre>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <p v-if="!visible.length" class="admin-muted">
            {{ t('noConflict') }}
          </p>
          <TablePagination
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="filtered.length"
          />
        </section>
      </template>
      <section v-if="result" class="admin-section" role="status">
        <h3>{{ t('syncFinished') }}</h3>
        <p>
          {{
            t('syncResult', {
              created: result.created_models,
              updated: result.updated_models,
              vendors: result.created_vendors,
            })
          }}
        </p>
        <template v-if="result.skipped_models.length"
          ><h4>{{ t('skipped') }}</h4>
          <ul class="sync-missing">
            <li v-for="name in result.skipped_models" :key="name">
              {{ name }}
            </li>
          </ul></template
        >
      </section>
    </div>
    <template #footer
      ><div class="sync-footer">
        <ConsoleButton variant="ghost" :disabled="saving" @click="emit('close')"
          ><X :size="16" />{{
            result ? t('close') : t('cancel')
          }}</ConsoleButton
        ><ConsoleButton
          v-if="preview"
          :loading="saving"
          :disabled="loading || (!preview.missing.length && !selected.length)"
          @click="synchronize"
          ><ArrowDownToLine :size="16" />{{ t('syncCreate') }}</ConsoleButton
        >
      </div></template
    >
  </ConsoleModal>
</template>
<style scoped>
.sync-section-title,
.sync-footer {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
  align-items: center;
  justify-content: space-between;
}
.sync-footer {
  justify-content: flex-end;
}
.sync-missing {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
.sync-missing li {
  max-width: 100%;
  overflow-wrap: anywhere;
  padding: 0.25rem 0.5rem;
  background: var(--surface-container-low);
  border-radius: var(--shape-small);
  font-size: 0.875rem;
}
.sync-table {
  table-layout: fixed;
}
.sync-table pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 10rem;
  overflow-y: auto;
  font: inherit;
}
@media (max-width: 640px) {
  .sync-table,
  .sync-table tbody {
    display: block;
  }
  .sync-table thead {
    display: none;
  }
  .sync-table tr {
    display: grid;
    margin-bottom: 1rem;
    border-bottom: 1px solid var(--border-default);
  }
  .sync-table td {
    display: block;
    max-width: none;
  }
  .sync-table td::before {
    content: attr(data-label);
    display: block;
    margin-bottom: 0.25rem;
    color: var(--text-tertiary);
    font-size: 0.75rem;
  }
}
</style>
