<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import type { UpstreamPlatform } from '@/api/upstreams'
import FormField from '@/components/common/FormField.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import TextInput from '@/components/common/TextInput.vue'
import { expiryModes, quotaModes, type KeyForm } from '../lib/upstreamKeyForm'

const props = defineProps<{ platform: UpstreamPlatform; editing: boolean }>()
const form = defineModel<KeyForm>('form', { required: true })
const { t } = useI18n()

const expiryOptions = computed(() =>
  expiryModes(props.platform, props.editing).map((value) => ({
    value,
    label: t(`upstreams.keyForm.expiryModes.${value}`),
  }))
)
const quotaOptions = computed(() =>
  quotaModes(props.editing).map((value) => ({
    value,
    label: t(`upstreams.keyForm.quotaModes.${value}`),
  }))
)
</script>

<template>
  <FormField :label="t('upstreams.keyForm.expiry')" group>
    <SegmentedToggle
      v-model="form.expiryMode"
      :options="expiryOptions"
      :label="t('upstreams.keyForm.expiry')"
      size="sm"
    />
  </FormField>
  <FormField
    v-if="form.expiryMode === 'date'"
    :label="t('upstreams.keyForm.expiresAt')"
  >
    <TextInput
      v-model="form.expiresAt"
      name="upstream-key-expires-at"
      type="datetime-local"
    />
  </FormField>
  <FormField
    v-if="form.expiryMode === 'days'"
    :label="t('upstreams.keyForm.expiresInDays')"
  >
    <TextInput
      v-model="form.days"
      name="upstream-key-days"
      inputmode="numeric"
    />
  </FormField>
  <FormField :label="t('upstreams.keyForm.quota')" group>
    <SegmentedToggle
      v-model="form.quotaMode"
      :options="quotaOptions"
      :label="t('upstreams.keyForm.quota')"
      size="sm"
    />
  </FormField>
  <FormField
    v-if="form.quotaMode === 'limited'"
    :label="
      platform === 'newapi'
        ? t('upstreams.keyForm.remainQuota')
        : t('upstreams.keyForm.sub2apiQuota')
    "
  >
    <TextInput
      v-model="form.quota"
      name="upstream-key-quota"
      inputmode="decimal"
    />
  </FormField>
</template>
