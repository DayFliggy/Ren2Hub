<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import TextInput from '@/components/common/TextInput.vue'
import FormField from '@/components/common/FormField.vue'
import { useSystemSettings } from '@/composables/useSystemSettings'

interface DomainChange {
  removed_rp_ids: string[]
  affected_credentials: number
  unknown_credentials: number
  confirmation_required: boolean
  removal_confirmation: string
}
const { t } = useI18n()
const settings = useSystemSettings()
const rpID = ref('')
const legacy = ref('')
const origins = ref('')
const preview = ref<DomainChange | null>(null)
const confirmed = ref(false)
const busy = ref(false)
const error = ref('')
watch(
  () => settings.rawOptions.value,
  () => {
    rpID.value = String(settings.rawValue('passkey.rp_id', ''))
    legacy.value = String(settings.rawValue('passkey.legacy_rp_ids', ''))
    origins.value = String(settings.rawValue('passkey.origins', ''))
  },
  { immediate: true }
)
watch([rpID, legacy, origins], () => {
  preview.value = null
  confirmed.value = false
})
async function submit(previewOnly: boolean) {
  busy.value = true
  error.value = ''
  try {
    const result = await api.put<DomainChange>('/api/option/passkey/domains', {
      rp_id: rpID.value,
      legacy_rp_ids: legacy.value,
      origins: origins.value,
      preview: previewOnly,
      removal_confirmation: previewOnly
        ? ''
        : (preview.value?.removal_confirmation ?? ''),
    })
    preview.value = result
    if (!previewOnly) {
      await settings.load(true)
      preview.value = null
      confirmed.value = false
    }
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : String(cause)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ConsoleCard class="sibling-space-y-4">
    <h3 class="font-semibold">{{ t('security.domainsTitle') }}</h3>
    <FormField label="RP ID"><TextInput v-model="rpID" /></FormField>
    <FormField :label="t('security.legacyDomains')"
      ><TextInput v-model="legacy"
    /></FormField>
    <FormField :label="t('security.origins')"
      ><TextInput v-model="origins"
    /></FormField>
    <div
      v-if="preview?.confirmation_required"
      class="sibling-space-y-2 rounded-lg border border-[var(--border-subtle)] p-3 text-sm"
    >
      <p>{{ t('security.domainsRemoved') }}</p>
      <p>
        {{ preview.removed_rp_ids.join(', ') }} ·
        {{ preview.affected_credentials }} / {{ preview.unknown_credentials }}
      </p>
      <label class="flex items-center gap-2"
        ><input v-model="confirmed" type="checkbox" />{{
          t('security.domainsConfirm')
        }}</label
      >
    </div>
    <p
      v-if="error"
      role="alert"
      class="text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <div class="flex flex-wrap gap-3">
      <ConsoleButton
        variant="secondary"
        :loading="busy"
        @click="submit(true)"
        >{{ t('security.domainsPreview') }}</ConsoleButton
      ><ConsoleButton
        :disabled="
          !preview || (preview.confirmation_required && !confirmed) || busy
        "
        @click="submit(false)"
        >{{ t('security.domainsApply') }}</ConsoleButton
      >
    </div>
  </ConsoleCard>
</template>
