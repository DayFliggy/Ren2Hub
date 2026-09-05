<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import {
  metadataApi,
  type SyncPreview,
  type SyncResult,
} from '@/api/adminManagement'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { adminManagementMessages } from '@/i18n/adminManagement'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: adminManagementMessages,
})
const { loading, error, run } = useAdminRequest()
const source = ref('official')
const language = ref(locale.value === 'zh-CN' ? 'zh' : 'en')
const preview = ref<SyncPreview | null>(null)
const result = ref<SyncResult | null>(null)
const selected = ref<string[]>([])
const saving = ref(false)
watch([source, language, () => props.open], () => {
  preview.value = null
  result.value = null
  selected.value = []
})
function selectionKey(model: string, field: string) {
  return JSON.stringify([model, field])
}
async function load() {
  await run(async (signal) => {
    const data = await metadataApi.preview(source.value, language.value, signal)
    if (signal.aborted) return
    preview.value = data
    selected.value = []
  })
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
              selectionKey(conflict.model_name, field.field)
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
            <option value="config">{{ t('config') }}</option>
          </select></label
        >
        <label class="admin-field"
          ><span>{{ t('locale') }}</span
          ><select v-model="language" :disabled="saving">
            <option value="zh">中文</option>
            <option value="en">English</option>
            <option value="ja">日本語</option>
          </select></label
        >
      </div>
      <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
      <ConsoleButton :loading="loading" :disabled="saving" @click="load">{{
        t('preview')
      }}</ConsoleButton>
      <template v-if="preview">
        <section>
          <h3>{{ t('missing') }} ({{ preview.missing.length }})</h3>
          <p class="admin-muted">
            {{ preview.missing.join(', ') || t('missingEmpty') }}
          </p>
        </section>
        <section>
          <h3>{{ t('overwrite') }}</h3>
          <div class="admin-table-scroll">
            <table class="admin-table">
              <thead>
                <tr>
                  <th>{{ t('modelName') }}</th>
                  <th>{{ t('field') }}</th>
                  <th>{{ t('local') }}</th>
                  <th>{{ t('upstream') }}</th>
                </tr>
              </thead>
              <tbody>
                <template
                  v-for="conflict in preview.conflicts"
                  :key="conflict.model_name"
                  ><tr v-for="field in conflict.fields" :key="field.field">
                    <td>
                      <label class="admin-check"
                        ><input
                          v-model="selected"
                          type="checkbox"
                          :value="
                            selectionKey(conflict.model_name, field.field)
                          "
                          :disabled="saving"
                        />{{ conflict.model_name }}</label
                      >
                    </td>
                    <td>{{ field.field }}</td>
                    <td>{{ field.local }}</td>
                    <td>{{ field.upstream }}</td>
                  </tr></template
                >
              </tbody>
            </table>
          </div>
          <p v-if="!preview.conflicts.length" class="admin-muted">
            {{ t('noConflict') }}
          </p>
        </section>
        <ConsoleButton
          :loading="saving"
          :disabled="loading || (!preview.missing.length && !selected.length)"
          @click="synchronize"
          >{{ t('syncCreate') }}</ConsoleButton
        >
      </template>
      <p v-if="result" role="status">
        {{
          t('syncResult', {
            created: result.created_models,
            updated: result.updated_models,
            vendors: result.created_vendors,
          })
        }}<span v-if="result.skipped_models.length">
          · {{ t('skipped') }}: {{ result.skipped_models.join(', ') }}</span
        >
      </p>
    </div>
  </ConsoleModal>
</template>
