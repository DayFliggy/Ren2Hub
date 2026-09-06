<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Save, X, RefreshCw } from 'lucide-vue-next'
import {
  metadataApi,
  type ModelInput,
  type ModelMetadata,
  type ModelVendor,
} from '@/api/adminManagement'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import MetadataIcon from './MetadataIcon.vue'
import StringListEditor from './StringListEditor.vue'
import MetadataEndpointEditor from './MetadataEndpointEditor.vue'
import ModelPricingEditor from './ModelPricingEditor.vue'
import { parseMetadataTags } from './modelMetadataForm'
import { modelMetadataMessages } from '@/i18n/modelMetadata'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  model: ModelMetadata | null
  initialName?: string
  vendors: ModelVendor[]
}>()
const emit = defineEmits<{ close: []; saved: []; changed: [] }>()
const { t, te } = useI18n({
  useScope: 'local',
  messages: modelMetadataMessages,
})
const auth = useAuthStore()
const form = reactive<ModelInput>({
  model_name: props.initialName ?? '',
  description: '',
  icon: '',
  tags: '',
  vendor_id: 0,
  endpoints: '',
  status: 1,
  sync_official: 1,
  name_rule: 0,
})
const tags = ref<string[]>([])
const endpointEditor = ref<InstanceType<typeof MetadataEndpointEditor> | null>(
  null
)
const pricingEditor = ref<InstanceType<typeof ModelPricingEditor> | null>(null)
const originalName = ref(props.model?.model_name ?? props.initialName ?? '')
const loading = ref(Boolean(props.model))
const saving = ref(false)
const error = ref('')
const metadataCommitted = ref(false)
const loadFailed = ref(false)
const controller = new AbortController()
const rules = ['exact', 'prefix', 'contains', 'suffix']
const vendorIcon = computed(
  () =>
    form.icon ||
    props.vendors.find((vendor) => vendor.id === form.vendor_id)?.icon ||
    props.vendors.find((vendor) => vendor.id === form.vendor_id)?.name ||
    form.model_name
)

async function load() {
  if (!props.model) return
  loading.value = true
  error.value = ''
  loadFailed.value = false
  try {
    const model = await metadataApi.model(props.model.id, controller.signal)
    if (controller.signal.aborted) return
    originalName.value = model.model_name
    Object.assign(form, {
      id: model.id,
      model_name: model.model_name,
      description: model.description,
      icon: model.icon,
      tags: model.tags,
      vendor_id: model.vendor_id,
      endpoints: model.endpoints,
      status: model.status,
      sync_official: model.sync_official,
      name_rule: model.name_rule,
    })
    tags.value = parseMetadataTags(model.tags)
  } catch (cause) {
    if (!controller.signal.aborted) {
      const message = cause instanceof Error ? cause.message : String(cause)
      error.value = te(message) ? t(message) : message
      loadFailed.value = true
    }
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}
onMounted(load)
onBeforeUnmount(() => controller.abort())

async function save() {
  if (saving.value || loading.value || loadFailed.value) return
  saving.value = true
  error.value = ''
  try {
    if (!metadataCommitted.value) {
      form.model_name = form.model_name.trim()
      if (!form.model_name) throw new Error(t('nameRequired'))
      const cleanTags = tags.value.map((tag) => tag.trim()).filter(Boolean)
      if (new Set(cleanTags).size !== cleanTags.length)
        throw new Error(t('duplicateTags'))
      form.tags = cleanTags.join(',')
      form.endpoints = endpointEditor.value?.validate() ?? form.endpoints
      await pricingEditor.value?.validate(form.model_name)
      const saved = await metadataApi.saveModel({ ...form })
      form.id = saved.id
      metadataCommitted.value = true
      emit('changed')
    }
    await pricingEditor.value?.save(form.model_name)
    emit('saved')
  } catch (cause) {
    const message = cause instanceof Error ? cause.message : String(cause)
    error.value = metadataCommitted.value
      ? `${t('partialSaved')} ${message}`
      : message
  } finally {
    saving.value = false
  }
}
</script>
<template>
  <ConsoleModal
    :open="true"
    :title="model ? t('edit') : t('create')"
    size="lg"
    presentation="drawer"
    :close-disabled="saving"
    @close="emit('close')"
  >
    <p v-if="loading" role="status" class="admin-muted">{{ t('loading') }}</p>
    <form
      v-else
      id="model-metadata-form"
      class="admin-form"
      @submit.prevent="save"
    >
      <p v-if="error" class="admin-error" role="alert">{{ error }}</p>
      <ConsoleButton v-if="loadFailed" variant="ghost" @click="load"
        ><RefreshCw :size="16" />{{ t('refresh') }}</ConsoleButton
      >
      <fieldset
        v-if="!loadFailed"
        class="admin-form min-w-0"
        :disabled="saving || metadataCommitted"
      >
        <div class="admin-fields">
          <label class="admin-field admin-field-wide"
            ><span>{{ t('model_name') }}</span
            ><input v-model="form.model_name" required autocomplete="off"
          /></label>
          <label class="admin-field"
            ><span>{{ t('vendor_id') }}</span
            ><select v-model.number="form.vendor_id">
              <option :value="0">{{ t('noVendor') }}</option>
              <option
                v-for="vendor in vendors"
                :key="vendor.id"
                :value="vendor.id"
              >
                {{ vendor.name }}
              </option>
            </select></label
          >
          <label class="admin-field"
            ><span>{{ t('name_rule') }}</span
            ><select v-model.number="form.name_rule">
              <option v-for="(rule, index) in rules" :key="rule" :value="index">
                {{ t(rule) }}
              </option>
            </select></label
          >
          <label class="admin-field admin-field-wide"
            ><span>{{ t('description') }}</span
            ><textarea v-model="form.description" />
          </label>
          <label class="admin-field admin-field-wide"
            ><span>{{ t('icon') }}</span
            ><span class="metadata-icon-input"
              ><input v-model="form.icon" /><span :aria-label="t('iconPreview')"
                ><MetadataIcon
                  :icon="vendorIcon"
                  :name="form.model_name"
                  :size="36" /></span></span
          ></label>
          <div class="admin-field admin-field-wide">
            <span>{{ t('tagsLabel') }}</span
            ><StringListEditor v-model="tags" :label="t('tags')" />
          </div>
          <label class="admin-check"
            ><input
              v-model="form.status"
              type="checkbox"
              :true-value="1"
              :false-value="0"
            />{{ t('enabled') }}</label
          >
          <label class="admin-check"
            ><input
              v-model="form.sync_official"
              type="checkbox"
              :true-value="1"
              :false-value="0"
            />{{ t('syncEnabled') }}</label
          >
        </div>
        <section class="admin-section">
          <h3>{{ t('endpoints') }}</h3>
          <MetadataEndpointEditor
            ref="endpointEditor"
            v-model="form.endpoints"
            :disabled="saving || metadataCommitted"
          />
        </section>
      </fieldset>
      <fieldset
        v-if="auth.isRoot && !loadFailed"
        :disabled="saving"
        class="min-w-0"
      >
        <ModelPricingEditor
          ref="pricingEditor"
          :open="true"
          :model-name="originalName"
        />
      </fieldset>
    </form>
    <template #footer
      ><div class="metadata-drawer-actions">
        <ConsoleButton variant="ghost" :disabled="saving" @click="emit('close')"
          ><X :size="16" />{{ t('cancel') }}</ConsoleButton
        ><ConsoleButton
          type="submit"
          form="model-metadata-form"
          :loading="saving"
          :disabled="loading || loadFailed"
          ><Save :size="16" />{{
            metadataCommitted ? t('retryPricing') : t('save')
          }}</ConsoleButton
        >
      </div></template
    >
  </ConsoleModal>
</template>
<style scoped>
.metadata-icon-input {
  display: flex;
  align-items: center;
  gap: 0.75rem;
}
.metadata-drawer-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.5rem;
}
</style>
