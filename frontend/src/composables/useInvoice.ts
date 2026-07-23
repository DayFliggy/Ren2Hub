import { onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import type { InvoiceItem } from '@/types/console'
import { useToast } from '@/composables/useToast'

export interface InvoiceForm {
  title: string
  tax_id: string
  amount: string
  email: string
  note: string
}

export function useInvoice() {
  const { t } = useI18n()
  const toast = useToast()

  const invoices = ref<InvoiceItem[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(10)
  const loading = ref(false)
  const submitting = ref(false)
  let loadController: AbortController | null = null
  let loadSequence = 0

  async function loadInvoices() {
    loadController?.abort()
    const controller = new AbortController()
    loadController = controller
    const sequence = ++loadSequence
    loading.value = true
    try {
      const res = await api.get<{ items: InvoiceItem[]; total: number }>(
        '/api/invoice/self',
        { page: page.value, page_size: pageSize.value },
        { signal: controller.signal }
      )
      if (sequence !== loadSequence) return
      invoices.value = res.items
      total.value = res.total
    } catch (error) {
      if (!controller.signal.aborted) {
        toast.error(error instanceof ApiError ? error.message : String(error))
      }
    } finally {
      if (sequence === loadSequence) loading.value = false
    }
  }

  async function submitApplication(form: InvoiceForm): Promise<boolean> {
    const titleTrimmed = form.title.trim()
    const amount = parseFloat(form.amount)

    if (!titleTrimmed) {
      toast.error(t('invoice.titleRequired'))
      return false
    }
    if (!form.amount || isNaN(amount) || amount < 200) {
      toast.error(t('invoice.amountMin'))
      return false
    }

    submitting.value = true
    try {
      await api.post('/api/invoice/apply', {
        title: titleTrimmed,
        tax_id: form.tax_id.trim(),
        amount,
        email: form.email.trim(),
        note: form.note.trim(),
      })
      toast.success(t('invoice.submitted'))
      // Refresh list to show the new record
      page.value = 1
      await loadInvoices()
      return true
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : String(error))
      return false
    } finally {
      submitting.value = false
    }
  }

  function goToPage(p: number) {
    page.value = p
    void loadInvoices()
  }

  onBeforeUnmount(() => loadController?.abort())

  return {
    invoices,
    total,
    page,
    pageSize,
    loading,
    submitting,
    loadInvoices,
    submitApplication,
    goToPage,
  }
}
