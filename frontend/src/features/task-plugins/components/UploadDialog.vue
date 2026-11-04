<script setup lang="ts">
// Upstream components/upload-dialog.tsx: risk warning, .js picker, URL
// import, source editor, declaration preview, sidecar icon and remark, then
// an activation step. Fields sent are exactly those UploadTaskPlugin binds.
// The declaration preview comes from the server metadata preview.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { AlertTriangle, CircleCheck, Power, Upload } from 'lucide-vue-next'

import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import TextInput from '@/components/common/TextInput.vue'
import { usePluginChangeNotice } from '../usePluginChangeNotice'

import { activateTaskPlugin, previewTaskPlugin, uploadTaskPlugin } from '../api'
import {
  encodePluginIconFile,
  PluginIconFileError,
} from '../lib/plugin-icon-file'
import {
  previewFromServer,
  unavailablePreview,
} from '../lib/plugin-meta-preview'
import {
  MAX_PLUGIN_SOURCE_BYTES,
  pluginSourceByteLength,
} from '../lib/plugin-url'
import type { PluginMetaPreview, TaskPluginDetail } from '../types'
import MarketplaceCapabilities from './MarketplaceCapabilities.vue'
import PluginAlert from './PluginAlert.vue'
import PluginIcon from './PluginIcon.vue'
import PluginSourcePicker from './PluginSourcePicker.vue'
import PluginUrlImportField from './PluginUrlImportField.vue'

const props = withDefaults(defineProps<{ initialKey?: string }>(), {
  initialKey: '',
})
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const notifyChange = usePluginChangeNotice()

const source = ref('')
const fileName = ref('')
const remark = ref('')
const icon = ref('')
const iconFileName = ref('')
const iconError = ref('')
const iconInput = ref<HTMLInputElement | null>(null)
const importUrl = ref('')
const importError = ref('')
// The hash of fetched URL text; dropped once the source is edited.
const fetched = ref<{ source: string; sha256: string } | null>(null)
const uploadError = ref('')
const uploading = ref(false)
const result = ref<TaskPluginDetail | null>(null)
const activating = ref(false)
const activated = ref(false)
const activationError = ref('')

// The server re-parses the whole source for every preview; let typing settle.
const PREVIEW_DELAY_MS = 300
const preview = ref<PluginMetaPreview | undefined>()
let previewTimer: ReturnType<typeof setTimeout> | undefined
let previewController: AbortController | undefined

const sha256 = computed(() =>
  fetched.value?.source === source.value ? fetched.value.sha256 : ''
)
const needsActivation = computed(
  () => result.value?.plugin?.active === false && !activated.value
)

function setSource(value: string) {
  source.value = value
  result.value = null
  uploadError.value = ''
}

async function loadPreview(text: string) {
  clearTimeout(previewTimer)
  previewController?.abort()
  const request = new AbortController()
  previewController = request
  try {
    const serverPreview = await previewTaskPlugin(text, request.signal)
    if (!request.signal.aborted)
      preview.value = previewFromServer(serverPreview)
  } catch {
    // A source the preview cannot parse shows as unreadable; the gateway
    // still validates it on upload.
    if (!request.signal.aborted) preview.value = unavailablePreview()
  }
}

watch(source, (text) => {
  clearTimeout(previewTimer)
  previewController?.abort()
  preview.value = undefined
  if (!text.trim()) return
  previewTimer = setTimeout(() => void loadPreview(text), PREVIEW_DELAY_MS)
})
onBeforeUnmount(() => {
  clearTimeout(previewTimer)
  previewController?.abort()
})

async function selectFile(file: File) {
  if (file.size > MAX_PLUGIN_SOURCE_BYTES) {
    importError.value = t('taskPlugins.sourceTooLarge')
    return
  }
  importError.value = ''
  fileName.value = file.name
  fetched.value = null
  setSource(await file.text())
}

function onFetched(text: string, digest: string | null) {
  fileName.value = ''
  fetched.value = digest ? { source: text, sha256: digest } : null
  setSource(text)
}

async function selectIcon(event: Event) {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  if (!file) return
  try {
    icon.value = await encodePluginIconFile(file)
    iconFileName.value = file.name
    iconError.value = ''
  } catch (error) {
    icon.value = ''
    iconFileName.value = ''
    iconError.value =
      error instanceof PluginIconFileError && error.reason === 'too_large'
        ? t('taskPlugins.iconTooLarge')
        : t('taskPlugins.iconType')
  }
  result.value = null
}

function clearIcon() {
  icon.value = ''
  iconFileName.value = ''
  iconError.value = ''
}

function chooseIcon() {
  iconInput.value?.click()
}

async function upload() {
  if (!source.value.trim() || uploading.value) return
  uploading.value = true
  uploadError.value = ''
  try {
    const detail = await uploadTaskPlugin({
      source: source.value,
      sourceSha256: sha256.value || undefined,
      remark: remark.value,
      icon: icon.value || undefined,
    })
    result.value = detail
    emit('changed')
    notifyChange(
      detail.plugin?.active === false
        ? t('taskPlugins.savedNeedsActivation')
        : t('taskPlugins.uploaded'),
      detail.pricing_issues
    )
  } catch (error) {
    // Verbatim: preflight rejections name the conflicting plugin.
    uploadError.value = error instanceof Error ? error.message : String(error)
  } finally {
    uploading.value = false
  }
}

async function activate() {
  if (!result.value || activating.value) return
  activating.value = true
  activationError.value = ''
  try {
    const notice = await activateTaskPlugin(
      result.value.meta.key,
      result.value.meta.version
    )
    activated.value = true
    emit('changed')
    notifyChange(t('taskPlugins.versionActivated'), notice?.pricing_issues)
  } catch (error) {
    activationError.value =
      error instanceof Error ? error.message : String(error)
  } finally {
    activating.value = false
  }
}

function close() {
  if (uploading.value || activating.value) return
  emit('close')
}
</script>

<template>
  <ConsoleModal
    v-if="result"
    :open="true"
    :title="t('taskPlugins.uploaded')"
    :subtitle="`${result.meta.name} (${result.meta.key}) · v${result.meta.version}`"
    size="md"
    :close-disabled="activating"
    @close="close"
  >
    <div class="sibling-space-y-3">
      <PluginAlert
        role="status"
        :tone="needsActivation ? 'warning' : 'success'"
        :title="
          needsActivation
            ? t('taskPlugins.savedNeedsActivation')
            : t('taskPlugins.versionActivated')
        "
      >
        <template #icon>
          <AlertTriangle v-if="needsActivation" :size="16" />
          <CircleCheck v-else :size="16" />
        </template>
        <template v-if="needsActivation">{{
          t('taskPlugins.needsActivationDesc')
        }}</template>
      </PluginAlert>
      <PluginAlert
        v-if="activationError"
        tone="danger"
        role="alert"
        :title="t('taskPlugins.activationFailed')"
      >
        <template #icon><AlertTriangle :size="16" /></template>
        <span class="whitespace-pre-wrap">{{ activationError }}</span>
      </PluginAlert>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton
          :variant="needsActivation ? 'secondary' : 'primary'"
          :disabled="activating"
          @click="close"
        >
          {{ needsActivation ? t('taskPlugins.later') : t('common.close') }}
        </ConsoleButton>
        <ConsoleButton
          v-if="needsActivation"
          :loading="activating"
          @click="activate"
        >
          <Power v-if="!activating" :size="15" aria-hidden="true" />
          {{
            activating
              ? t('taskPlugins.activating')
              : t('taskPlugins.activateNow')
          }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
  <ConsoleModal
    v-else
    :open="true"
    :title="
      props.initialKey
        ? t('taskPlugins.uploadNewPluginVersion')
        : t('taskPlugins.uploadTask')
    "
    :subtitle="
      props.initialKey
        ? `${t('taskPlugins.pluginKey')}: ${props.initialKey}`
        : t('taskPlugins.uploadDesc')
    "
    size="lg"
    :close-disabled="uploading"
    @close="close"
  >
    <div class="sibling-space-y-4">
      <PluginAlert tone="danger" :title="t('taskPlugins.riskTitle')">
        <template #icon><AlertTriangle :size="16" /></template>
        {{ t('taskPlugins.riskDesc') }}
      </PluginAlert>
      <PluginSourcePicker :file-name="fileName" @select="selectFile" />
      <PluginUrlImportField
        v-model="importUrl"
        :error="importError"
        :sha256="sha256"
        @error="importError = $event"
        @fetched="onFetched"
      />
      <div class="sibling-space-y-1.5">
        <div class="flex items-center justify-between gap-2">
          <label
            for="task-plugin-source"
            class="text-sm font-medium text-[var(--text-secondary)]"
            >{{ t('taskPlugins.pluginSource') }}</label
          >
          <span class="font-mono text-[11px] text-[var(--text-tertiary)]">{{
            t('taskPlugins.bytes', { bytes: pluginSourceByteLength(source) })
          }}</span>
        </div>
        <textarea
          id="task-plugin-source"
          :value="source"
          rows="14"
          spellcheck="false"
          :placeholder="t('taskPlugins.pastePlaceholder')"
          class="w-full rounded-xl border border-[var(--border-default)] bg-[var(--surface-solid)] p-3 font-mono text-xs text-[var(--text-primary)] outline-hidden placeholder:text-[var(--text-tertiary)] focus:border-[var(--accent)] focus-ring"
          @input="setSource(($event.target as HTMLTextAreaElement).value)"
        />
      </div>
      <MarketplaceCapabilities
        v-if="preview"
        :preview="preview"
        @retry="loadPreview(source)"
      />
      <div class="sibling-space-y-1.5">
        <label
          for="task-plugin-icon"
          class="block text-sm font-medium text-[var(--text-secondary)]"
          >{{ t('taskPlugins.pluginIcon') }}</label
        >
        <div class="flex flex-wrap items-center gap-3">
          <PluginIcon
            v-if="icon"
            :plugin="{ key: 'upload', iconSrc: icon }"
            :size="32"
          />
          <ConsoleButton
            variant="secondary"
            size="sm"
            :aria-invalid="iconError ? true : undefined"
            @click="chooseIcon"
          >
            <Upload :size="14" aria-hidden="true" />
            {{
              iconFileName
                ? t('taskPlugins.chooseAnother')
                : t('taskPlugins.chooseFile')
            }}
          </ConsoleButton>
          <input
            id="task-plugin-icon"
            ref="iconInput"
            type="file"
            accept=".svg,.png,image/svg+xml,image/png"
            class="sr-only"
            :aria-label="t('taskPlugins.pluginIcon')"
            @change="selectIcon"
          />
          <ConsoleButton
            v-if="icon"
            variant="ghost"
            size="sm"
            @click="clearIcon"
          >
            {{ t('taskPlugins.remove') }}
          </ConsoleButton>
        </div>
        <p class="break-all text-xs text-[var(--text-tertiary)]">
          {{ iconFileName || t('taskPlugins.iconHint') }}
        </p>
        <p
          v-if="iconError"
          role="alert"
          class="text-xs text-[var(--status-danger-text)]"
        >
          {{ iconError }}
        </p>
      </div>
      <div class="sibling-space-y-1.5">
        <label
          for="task-plugin-remark"
          class="block text-sm font-medium text-[var(--text-secondary)]"
          >{{ t('taskPlugins.remark') }}</label
        >
        <TextInput
          id="task-plugin-remark"
          v-model="remark"
          :placeholder="t('taskPlugins.remarkPlaceholder')"
        />
      </div>
      <PluginAlert
        v-if="uploadError"
        tone="danger"
        role="alert"
        :title="t('taskPlugins.rejected')"
      >
        <template #icon><AlertTriangle :size="16" /></template>
        <span class="whitespace-pre-wrap">{{ uploadError }}</span>
      </PluginAlert>
    </div>
    <template #footer>
      <div class="flex flex-wrap justify-end gap-2">
        <ConsoleButton variant="secondary" :disabled="uploading" @click="close">
          {{ t('common.close') }}
        </ConsoleButton>
        <ConsoleButton
          :disabled="!source.trim()"
          :loading="uploading"
          @click="upload"
        >
          <Upload v-if="!uploading" :size="15" aria-hidden="true" />
          {{ uploading ? t('taskPlugins.uploading') : t('taskPlugins.upload') }}
        </ConsoleButton>
      </div>
    </template>
  </ConsoleModal>
</template>
