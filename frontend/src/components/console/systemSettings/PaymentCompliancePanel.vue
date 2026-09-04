<script setup lang="ts">
import { computed, ref } from 'vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { api } from '@/api/console'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { useToast } from '@/composables/useToast'

const emit = defineEmits<{ confirmed: [] }>()
const { rawValue } = useSystemSettings()
const toast = useToast()
const open = ref(false)
const loading = ref(false)
const confirmed = computed(() =>
  Boolean(rawValue('payment_setting.compliance_confirmed', false))
)
const termsVersion = computed(() =>
  String(rawValue('payment_setting.compliance_terms_version', 'v1'))
)
const confirmedAt = computed(() =>
  Number(rawValue('payment_setting.compliance_confirmed_at', 0))
)

async function confirm() {
  loading.value = true
  try {
    await api.post('/api/option/payment_compliance', { confirmed: true })
    open.value = false
    toast.success('支付合规已确认')
    emit('confirmed')
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
      <h3>支付合规</h3>
      <p v-if="confirmed">
        已确认条款 {{ termsVersion
        }}<span v-if="confirmedAt"
          >，时间 {{ new Date(confirmedAt * 1000).toLocaleString() }}</span
        >。
      </p>
      <p v-else>启用邀请奖励和在线支付前，必须完成合规确认。</p>
    </div>
    <ConsoleButton v-if="!confirmed" size="sm" @click="open = true"
      >确认合规条款</ConsoleButton
    >
    <span v-else class="compliance-badge">已确认</span>
  </section>
  <ConfirmDialog
    :open="open"
    title="确认支付合规条款"
    message="确认你已阅读并同意当前支付合规条款。该操作会记录管理员和时间。"
    confirm-text="确认并记录"
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
  border-top: 1px dashed var(--border-default);
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
