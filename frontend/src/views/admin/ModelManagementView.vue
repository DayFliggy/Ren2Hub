<script setup lang="ts">
/**
 * NewAPI model management: "Models" (/models/metadata) and "Vendors"
 * (/models/vendors) sections. The retired Deployments section is not shown.
 */
import { computed, nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AlertCircle, List, Plus, RefreshCw } from 'lucide-vue-next'
import { metadataApi, type ModelVendor } from '@/api/adminManagement'
import ActionMenu from '@/components/common/ActionMenu.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleTabs from '@/components/common/ConsoleTabs.vue'
import PageBreadcrumb from '@/components/console/PageBreadcrumb.vue'
import MetadataSyncDialog from '@/components/admin/MetadataSyncDialog.vue'
import MissingModelsDialog from '@/components/admin/MissingModelsDialog.vue'
import ModelMetadataDrawer from '@/components/admin/ModelMetadataDrawer.vue'
import ModelsSection from '@/components/admin/ModelsSection.vue'
import PrefillGroupDialog from '@/components/admin/PrefillGroupDialog.vue'
import VendorsSection from '@/components/admin/VendorsSection.vue'
import { useAdminRequest } from '@/composables/useAdminRequest'
import { modelMetadataMessages } from '@/i18n/modelMetadata'

const props = defineProps<{ section: 'metadata' | 'vendors' }>()
const { t } = useI18n({ useScope: 'local', messages: modelMetadataMessages })
const { t: tGlobal } = useI18n()
const route = useRoute()
const router = useRouter()
const vendorRequest = useAdminRequest()
const vendors = ref<ModelVendor[]>([])
const modelsSection = ref<InstanceType<typeof ModelsSection> | null>(null)
const vendorsSection = ref<InstanceType<typeof VendorsSection> | null>(null)
const drawerOpen = ref(false)
const editingId = ref<number | null>(null)
const initialName = ref('')
const syncOpen = ref(false)
const missingOpen = ref(false)

const tabs = computed(() => [
  { key: 'metadata', label: t('models') },
  { key: 'vendors', label: t('vendors') },
])
const prefillOpen = computed(() => route.query.manage === 'prefill-groups')
const moreItems = computed(() => [
  { key: 'missing', label: t('missingModels'), icon: AlertCircle },
  { key: 'prefill', label: t('prefillGroups'), icon: List, separated: true },
])

async function loadVendors() {
  await vendorRequest.run(async (signal) => {
    const result = await metadataApi.allVendors(signal)
    if (!signal.aborted) vendors.value = result
  })
}
function switchSection(section: string) {
  if (section === props.section) return
  // Each section reads only its own query keys, as in NewAPI.
  void router.push({
    path: `/models/${section}`,
    query: route.query,
    hash: route.hash,
  })
}
function openDrawer(id: number | null, name = '') {
  editingId.value = id
  initialName.value = name
  drawerOpen.value = true
}
function createFromMissing(name: string) {
  missingOpen.value = false
  openDrawer(null, name)
}
function syncFromMissing() {
  missingOpen.value = false
  syncOpen.value = true
}
function selectMore(key: string) {
  if (key === 'missing') missingOpen.value = true
  else
    void router.replace({
      query: { ...route.query, manage: 'prefill-groups' },
      hash: route.hash,
    })
}
function closePrefill() {
  void router.replace({
    query: { ...route.query, manage: undefined },
    hash: route.hash,
  })
}
async function modelSaved() {
  drawerOpen.value = false
  await nextTick()
  if (props.section === 'vendors') await vendorsSection.value?.reload()
  else await modelsSection.value?.reload()
}
async function metadataChanged() {
  await loadVendors()
  await modelsSection.value?.reload()
}

onMounted(loadVendors)
</script>
<template>
  <div>
    <PageBreadcrumb
      :crumbs="[
        tGlobal('nav.groupAdmin'),
        t(section === 'vendors' ? 'vendorManagement' : 'modelManagement'),
      ]"
    >
      <template #action>
        <div
          v-if="section === 'metadata'"
          class="flex flex-wrap items-center gap-2"
        >
          <ConsoleButton variant="secondary" size="sm" @click="syncOpen = true">
            {{ t('syncMetadata') }}
          </ConsoleButton>
          <ConsoleButton size="sm" @click="openDrawer(null)">
            <Plus :size="14" aria-hidden="true" />{{ t('addModel') }}
          </ConsoleButton>
          <ActionMenu
            :items="moreItems"
            :label="t('openMenu')"
            variant="outline"
            @select="selectMore"
          />
        </div>
        <ConsoleButton v-else size="sm" @click="vendorsSection?.openCreate()">
          <Plus :size="14" aria-hidden="true" />{{ t('addVendor') }}
        </ConsoleButton>
      </template>
    </PageBreadcrumb>
    <ConsoleTabs
      :model-value="section"
      :items="tabs"
      panel-id="model-management-panel"
      class="mb-5"
      :aria-label="t('sections')"
      @update:model-value="switchSection"
    />
    <div
      v-if="vendorRequest.error.value"
      role="alert"
      class="mb-4 flex flex-wrap items-center gap-3 rounded-xl border border-[var(--border-subtle)] px-4 py-2.5 text-sm text-[var(--status-danger-text)]"
    >
      <span class="mr-auto">{{ vendorRequest.error.value }}</span>
      <ConsoleButton
        variant="ghost"
        size="sm"
        :loading="vendorRequest.loading.value"
        @click="loadVendors"
        ><RefreshCw :size="14" aria-hidden="true" />{{
          t('retry')
        }}</ConsoleButton
      >
    </div>
    <div
      id="model-management-panel"
      role="tabpanel"
      :aria-labelledby="`model-management-panel-tab-${section}`"
    >
      <ModelsSection
        v-if="section === 'metadata'"
        ref="modelsSection"
        :vendors="vendors"
        @edit="openDrawer"
      />
      <VendorsSection
        v-else
        ref="vendorsSection"
        @changed="loadVendors"
        @open-model="openDrawer($event.id)"
      />
    </div>
    <ModelMetadataDrawer
      v-if="drawerOpen"
      :model-id="editingId"
      :initial-name="initialName"
      :vendors="vendors"
      @close="drawerOpen = false"
      @saved="modelSaved"
    />
    <MetadataSyncDialog
      :open="syncOpen"
      @close="syncOpen = false"
      @saved="metadataChanged"
    />
    <MissingModelsDialog
      :open="missingOpen"
      @close="missingOpen = false"
      @create="createFromMissing"
      @sync="syncFromMissing"
    />
    <!-- Prefill groups feed channel forms; nothing on this page reloads. -->
    <PrefillGroupDialog :open="prefillOpen" @close="closePrefill" />
  </div>
</template>
