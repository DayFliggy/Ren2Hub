<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { api } from '@/api/console'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'

const emit = defineEmits<{ confirmed: [] }>()
const { rawValue, refreshStatus } = useSystemSettings()
const { t, locale } = useI18n()
const toast = useToast()
const open = ref(false)
const loading = ref(false)
const confirmed = computed(
  () => rawValue('payment_setting.compliance_confirmed', false) === true
)
const termsVersion = computed(() =>
  String(rawValue('payment_setting.compliance_terms_version', 'v1'))
)
const confirmedAt = computed(() =>
  Number(rawValue('payment_setting.compliance_confirmed_at', 0))
)
const confirmedText = computed(() =>
  confirmedAt.value
    ? t('systemSettings.paymentCompliance.confirmedTermsAt', {
        version: termsVersion.value,
        time: new Date(confirmedAt.value * 1000).toLocaleString(locale.value),
      })
    : t('systemSettings.paymentCompliance.confirmedTerms', {
        version: termsVersion.value,
      })
)

async function confirm() {
  loading.value = true
  try {
    await api.post('/api/option/payment_compliance', { confirmed: true })
    open.value = false
    toast.success(t('systemSettings.paymentCompliance.saved'))
    emit('confirmed')
    await refreshStatus(true)
  } catch (error) {
    toast.error(error instanceof Error ? error.message : String(error))
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <section class="compliance-panel">
    <div>
      <h3>{{ t('systemSettings.paymentCompliance.title') }}</h3>
      <p v-if="confirmed">{{ confirmedText }}</p>
      <p v-else>{{ t('systemSettings.paymentCompliance.required') }}</p>
    </div>
    <ConsoleButton v-if="!confirmed" size="sm" @click="open = true">{{
      t('systemSettings.paymentCompliance.confirmTerms')
    }}</ConsoleButton>
    <span v-else class="compliance-badge">{{
      t('systemSettings.paymentCompliance.confirmed')
    }}</span>
  </section>
  <ConfirmDialog
    :open="open"
    :title="t('systemSettings.paymentCompliance.dialogTitle')"
    :message="t('systemSettings.paymentCompliance.dialogMessage')"
    :confirm-text="t('systemSettings.paymentCompliance.dialogConfirm')"
    :loading="loading"
    @cancel="open = false"
    @confirm="confirm"
  />
</template>

<style scoped>
.compliance-panel {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  margin-top: 1.5rem;
  border-top: 1px var(--row-divider-style, dashed) var(--border-default);
  padding-top: 1rem;
}
.compliance-panel h3 {
  color: var(--text-primary);
  font-weight: 700;
}
.compliance-panel p {
  margin-top: 0.25rem;
  font-size: 0.75rem;
  color: var(--text-tertiary);
}
.compliance-badge {
  color: var(--signal);
  font-size: 0.75rem;
  font-weight: 700;
}
@media (max-width: 767px) {
  .compliance-panel {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
