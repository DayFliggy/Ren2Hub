import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/client'
import { channelOperation, parseChannelPage } from '@/api/channelStatus'
import { hasPendingPublication } from '@/api/publication'
import {
  invalidResponse,
  isRecord,
  requiredString,
  requiredStrictInteger,
  parseStringArray,
  requiredStrictNumber,
} from '@/api/contracts'
import { errorMessage } from '@/api/clientError'
import { ApiError, type ApiEnvelope } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { useAuthStore } from '@/stores/auth'
import { useToast } from '@/composables/useToast'
import {
  isRetiredChannelType,
  supportsBalanceQuery,
} from '@/constants/adminChannels'
import type {
  AdminChannel,
  AdminChannelCreateInput,
  AdminChannelCreateOptions,
  AdminChannelCreateResult,
  AdminChannelFetchModelsParams,
  AdminChannelSortBy,
  AdminChannelSortOrder,
  AdminChannelUpdateInput,
} from '@/types/console'

type ChannelAction = 'priority' | 'weight' | 'status' | 'balance'
type ChannelCrudAction = 'create' | 'edit' | 'delete'
type ChannelBatchScope = 'page' | 'supplier'
type ChannelBulkAction = 'delete' | 'enable' | 'disable' | 'reset'

/** Batch runs are balance-only; model testing lives in the test modal. */
interface ChannelBatchProgress {
  scope: ChannelBatchScope
  supplier?: string
  total: number
  processed: number
  succeeded: number
  failed: number
}

export interface ChannelModelTestOptions {
  endpointType?: string
  stream?: boolean
}

export interface ChannelModelTestResult {
  ok: boolean
  timeMs?: number
  message?: string
}

const CHANNEL_BATCH_SIZE = 5
/** Returned by a row action whose request the server answered as a no-op. */
const UNCHANGED = Symbol('unchanged')
/** Stable channel error codes shown in the console language. */
const CHANNEL_ERROR_KEYS: Readonly<Record<string, string>> = {
  CHANNEL_NOT_FOUND: 'channels.errors.notFound',
  CHANNEL_STATUS_UPDATE_FAILED: 'channels.errors.statusUpdateFailed',
  CHANNEL_REDACTED_FIELD_INVALID: 'channels.errors.redactedFieldInvalid',
}

type BatchStatusFailure = {
  changed: number
  envelope: Record<string, unknown>
  failures: { id: number; code: string }[]
}

/** A batch that applied only some ids answers success:false with failures. */
function batchStatusFailure(error: unknown): BatchStatusFailure | null {
  if (!(error instanceof ApiError) || !error.business) return null
  const envelope = error.envelope
  if (!isRecord(envelope) || !Array.isArray(envelope.failures)) return null
  const failures = envelope.failures.map((item) =>
    isRecord(item) &&
    Number.isSafeInteger(item.id) &&
    typeof item.code === 'string'
      ? { id: item.id as number, code: item.code }
      : null
  )
  if (
    failures.length === 0 ||
    failures.includes(null) ||
    !Number.isSafeInteger(envelope.data)
  )
    return null
  return {
    changed: envelope.data as number,
    envelope,
    failures: failures as BatchStatusFailure['failures'],
  }
}

export function useAdminChannels() {
  const { t } = useI18n()
  const toast = useToast()
  const auth = useAuthStore()
  const allowed = (action: string) => auth.hasPermission('channel', action)
  const canRead = computed(() => allowed('read'))
  const channelErrorMessage = (error: unknown): string => {
    const key =
      error instanceof ApiError && error.code
        ? CHANNEL_ERROR_KEYS[error.code]
        : undefined
    return key ? t(key) : errorMessage(error, t)
  }

  const rows = ref<AdminChannel[]>([])
  const total = ref(0)
  const typeCounts = ref<Record<string, number>>({})
  const page = ref(1)
  const pageSize = ref(20)
  const keyword = ref('')
  const modelKeyword = ref('')
  const type = ref('')
  const lab = ref('')
  const labs = ref<
    { id: string; name: string; source: string; count: number }[]
  >([])
  const status = ref('')
  const sortBy = ref<AdminChannelSortBy>('id')
  const sortOrder = ref<AdminChannelSortOrder>('desc')
  const loading = ref(true)
  const refreshing = ref(false)
  const initialError = ref('')
  const busy = ref<Map<number, ChannelAction>>(new Map())
  const batchProgress = ref<ChannelBatchProgress | null>(null)
  const balanceResponse = ref<{
    channelName: string
    rawResponse: string
  } | null>(null)
  const crudAction = ref<{
    action: ChannelCrudAction
    id: number | null
  } | null>(null)
  const bulkAction = ref<ChannelBulkAction | null>(null)
  const isBatchBusy = computed(() => batchProgress.value !== null)
  const isCrudBusy = computed(() => crudAction.value !== null)
  const isBulkBusy = computed(() => bulkAction.value !== null)
  const canMutate = computed(
    () =>
      canRead.value &&
      !loading.value &&
      !refreshing.value &&
      !isBatchBusy.value &&
      !isCrudBusy.value &&
      !isBulkBusy.value &&
      busy.value.size === 0
  )
  const canRunBatch = computed(
    () =>
      allowed('operate') &&
      rows.value.some(supportsBalanceQuery) &&
      canMutate.value
  )

  let batchController: AbortController | null = null
  let crudController: AbortController | null = null
  let bulkController: AbortController | null = null
  const listRequest = useLatestRequest()
  let searchTimer = 0

  function isBusy(id: number, action: ChannelAction): boolean {
    return busy.value.get(id) === action
  }

  function isRowBusy(id: number): boolean {
    return (
      isBatchBusy.value ||
      isCrudBusy.value ||
      isBulkBusy.value ||
      busy.value.has(id)
    )
  }

  function isCrudActionBusy(action: ChannelCrudAction, id?: number): boolean {
    return (
      crudAction.value?.action === action &&
      (id === undefined || crudAction.value.id === id)
    )
  }

  function isBulkActionBusy(action: ChannelBulkAction): boolean {
    return bulkAction.value === action
  }

  function setBusy(id: number, action: ChannelAction | null) {
    const next = new Map(busy.value)
    if (action) next.set(id, action)
    else next.delete(id)
    busy.value = next
  }

  function setBatchRowsBusy(channels: AdminChannel[], busyNow: boolean) {
    const next = new Map(busy.value)
    channels.forEach((channel) => {
      if (busyNow) next.set(channel.id, 'balance')
      else next.delete(channel.id)
    })
    busy.value = next
  }

  function currentParams() {
    return {
      p: page.value,
      page_size: pageSize.value,
      type: type.value || undefined,
      lab: lab.value || undefined,
      status: status.value || undefined,
      sort_by: sortBy.value,
      sort_order: sortOrder.value,
      keyword: keyword.value.trim() || undefined,
      model: modelKeyword.value.trim() || undefined,
    }
  }

  const hasFilters = computed(() =>
    [keyword, modelKeyword, type, lab, status].some((filter) =>
      filter.value.trim()
    )
  )

  /** Clears every list filter; sorting is a view preference and stays. */
  function resetFilters() {
    keyword.value = ''
    modelKeyword.value = ''
    type.value = ''
    lab.value = ''
    status.value = ''
  }

  /**
   * Resolves false when this refresh failed and the rows are stale. A request
   * superseded by a newer load resolves true: the newer load owns the rows and
   * reports its own failure.
   */
  async function load(
    options: { background?: boolean } = {}
  ): Promise<boolean> {
    if (!canRead.value) return false
    const background = options.background === true && rows.value.length > 0

    if (background) refreshing.value = true
    else loading.value = true
    if (rows.value.length === 0) initialError.value = ''

    // Native GetAllChannels ignores model; only SearchChannels filters it.
    const endpoint =
      keyword.value.trim() || modelKeyword.value.trim()
        ? '/api/channel/search'
        : '/api/channel/'
    const result = await listRequest.run(async (signal) => {
      const response = await api.get<unknown>(endpoint, currentParams(), {
        signal,
      })
      const categories = await api.get<{
        items: { id: string; name: string; source: string; count: number }[]
      }>('/api/channel/labs', undefined, { signal })
      labs.value = categories.items
      return parseChannelPage(response)
    })
    if (result.stale) return true
    loading.value = false
    refreshing.value = false
    if (!result.ok) {
      const message =
        result.error instanceof ApiError
          ? result.error.message
          : String(result.error)
      if (rows.value.length === 0) initialError.value = message
      else toast.error(message)
      return false
    }
    rows.value = result.value.items
    total.value = result.value.total
    typeCounts.value = result.value.type_counts
    initialError.value = ''
    return true
  }

  function reloadFromFirstPage() {
    window.clearTimeout(searchTimer)
    if (page.value === 1) void load()
    else page.value = 1
  }

  watch([keyword, modelKeyword], () => {
    window.clearTimeout(searchTimer)
    searchTimer = window.setTimeout(reloadFromFirstPage, 300)
  })
  watch([type, lab, status, sortBy, sortOrder], reloadFromFirstPage)
  watch(pageSize, reloadFromFirstPage)
  watch(page, () => void load())

  async function runChannelAction(
    id: number,
    action: ChannelAction,
    request: () => Promise<unknown>,
    successKey: string
  ): Promise<boolean> {
    const permission =
      action === 'priority' || action === 'weight' ? 'write' : 'operate'
    if (!canMutate.value || !allowed(permission) || isRowBusy(id)) return false
    setBusy(id, action)
    try {
      const outcome = await request()
      const refreshed = await load({ background: true })
      if (outcome === UNCHANGED) {
        toast.warning(
          t(
            refreshed
              ? 'channels.statusUnchanged'
              : 'channels.statusUnchangedRefreshFailed'
          )
        )
        return false
      }
      toast.success(t(successKey))
      return true
    } catch (error) {
      toast.error(channelErrorMessage(error))
      return false
    } finally {
      setBusy(id, null)
    }
  }

  function updateNumber(
    channel: AdminChannel,
    field: 'priority' | 'weight',
    value: number
  ): Promise<boolean> {
    if (isRetiredChannelType(channel.type) || !Number.isSafeInteger(value))
      return Promise.resolve(false)
    return runChannelAction(
      channel.id,
      field,
      async () => {
        if (field === 'weight')
          return api.put<unknown>('/api/channel/', {
            id: channel.id,
            weight: value,
          })
        const endpoint = '/api/channel/priority/order'
        const snapshot = await api.get<unknown>(endpoint)
        if (!isRecord(snapshot) || !Array.isArray(snapshot.items))
          invalidResponse(endpoint)
        const revision = requiredString(snapshot.revision, endpoint, false)
        const items = snapshot.items.map((item) => {
          if (!isRecord(item)) invalidResponse(endpoint)
          return {
            id: requiredStrictInteger(item.id, endpoint),
            priority:
              item.priority === null
                ? null
                : requiredStrictInteger(item.priority, endpoint),
          }
        })
        const current = items.find((item) => item.id === channel.id)
        if (!current || (current.priority ?? 0) !== channel.priority)
          throw new ApiError(t('channels.priorityConflict'), {
            status: 409,
            code: 'CHANNEL_PRIORITY_CONFLICT',
          })
        const input = {
          revision,
          restore: [
            { id: channel.id, previous: current.priority, priority: value },
          ],
        }
        await api.post('/api/channel/priority/reorder', {
          ...input,
          preview: true,
        })
        await api.post('/api/channel/priority/reorder', {
          ...input,
          preview: false,
        })
      },
      'channels.updated'
    )
  }

  function toggleStatus(channel: AdminChannel): Promise<boolean> {
    if (isRetiredChannelType(channel.type)) return Promise.resolve(false)
    const nextStatus = channel.status === 1 ? 2 : 1
    return runChannelAction(
      channel.id,
      'status',
      async () => {
        const response = await api.postEnvelope<unknown>(
          `/api/channel/${channel.id}/status`,
          {
            status: nextStatus,
          }
        )
        if (hasPendingPublication(response))
          toast.warning(t('common.publicationPending'))
        // data is false when the stored status was already the target or the
        // write did not apply; the reloaded list shows the server's status.
        if (response.data === false) return UNCHANGED
      },
      nextStatus === 1 ? 'channels.enabled' : 'channels.disabled'
    )
  }

  function refreshBalance(channel: AdminChannel): Promise<boolean> {
    if (!supportsBalanceQuery(channel)) return Promise.resolve(false)
    return runChannelAction(
      channel.id,
      'balance',
      async () => {
        const result = await channelOperation(
          `/api/channel/update_balance/${channel.id}`
        )
        if (typeof result.raw_response === 'string') {
          balanceResponse.value = {
            channelName: channel.name,
            rawResponse: result.raw_response,
          }
        }
      },
      'channels.balanceQueryCompleted'
    )
  }

  /**
   * Test one model on a channel, optionally overriding the endpoint and
   * stream mode. Failures come back as a result (not a throw) so the test
   * modal can render per-model outcomes; only aborts reject.
   */
  async function testChannelModel(
    channel: AdminChannel,
    model: string,
    options: ChannelModelTestOptions = {},
    signal?: AbortSignal
  ): Promise<ChannelModelTestResult> {
    if (!allowed('operate'))
      return { ok: false, message: t('channels.permissionDenied') }
    if (isRetiredChannelType(channel.type))
      return { ok: false, message: t('channels.retiredHint') }
    try {
      const endpoint = `/api/channel/test/${channel.id}`
      const data = await channelOperation(
        endpoint,
        {
          model,
          ...(options.endpointType
            ? { endpoint_type: options.endpointType }
            : {}),
          ...(options.stream ? { stream: 'true' } : {}),
        },
        signal
      )
      const seconds = requiredStrictNumber(data.time, endpoint)
      if (seconds < 0) invalidResponse(endpoint)
      return { ok: true, timeMs: Math.round(seconds * 1000) }
    } catch (error) {
      if (signal?.aborted) throw error
      return {
        ok: false,
        message: error instanceof ApiError ? error.message : String(error),
      }
    }
  }

  /**
   * Strip the given models from a channel's published list (used by the test
   * modal's "remove failed models"). Refuses to empty the list because the
   * backend treats an empty models update as "unchanged".
   */
  function removeChannelModels(
    channel: AdminChannel,
    modelsToRemove: string[]
  ): Promise<boolean> {
    if (isRetiredChannelType(channel.type)) return Promise.resolve(false)
    const removal = new Set(modelsToRemove)
    const remaining = channel.models
      .split(',')
      .map((model) => model.trim())
      .filter((model) => model.length > 0 && !removal.has(model))
    if (remaining.length === 0) {
      toast.error(t('channels.removeFailedBlocked'))
      return Promise.resolve(false)
    }
    return runCrudAction('edit', channel.id, async (signal) => {
      await api.put<unknown>(
        '/api/channel/',
        { id: channel.id, models: remaining.join(',') },
        { signal }
      )
      toast.success(
        t('channels.modelsRemoved', { count: modelsToRemove.length })
      )
      await load({ background: true })
    })
  }

  async function runCrudAction(
    action: ChannelCrudAction,
    id: number | null,
    request: (signal: AbortSignal) => Promise<void>
  ): Promise<boolean> {
    if (
      !canMutate.value ||
      !allowed(action === 'edit' ? 'write' : 'sensitive_write')
    )
      return false

    const controller = new AbortController()
    crudController = controller
    crudAction.value = { action, id }
    try {
      await request(controller.signal)
      return true
    } catch (error) {
      if (controller.signal.aborted) return false
      toast.error(channelErrorMessage(error))
      return false
    } finally {
      if (crudController === controller) crudController = null
      crudAction.value = null
    }
  }

  function createChannel(
    input: AdminChannelCreateInput,
    options?: AdminChannelCreateOptions
  ): Promise<boolean> {
    if (isRetiredChannelType(input.type)) return Promise.resolve(false)
    return runCrudAction('create', null, async (signal) => {
      await api.post<AdminChannelCreateResult>(
        '/api/channel/',
        {
          mode: options?.batchKeys ? 'batch' : 'single',
          channel: input,
        },
        { signal }
      )
      toast.success(t('channels.created'))
      if (page.value !== 1) page.value = 1
      else await load({ background: true })
    })
  }

  function updateChannelDetails(
    channel: AdminChannel,
    input: AdminChannelUpdateInput
  ): Promise<boolean> {
    // input.type is absent when the editor lacks ChannelSensitiveWrite.
    if (
      isRetiredChannelType(channel.type) ||
      (input.type !== undefined && isRetiredChannelType(input.type))
    )
      return Promise.resolve(false)
    return runCrudAction('edit', channel.id, async (signal) => {
      await api.put<unknown>(
        '/api/channel/',
        { id: channel.id, ...input },
        { signal }
      )
      toast.success(t('channels.updated'))
      await load({ background: true })
    })
  }

  function deleteChannel(channel: AdminChannel): Promise<boolean> {
    return runCrudAction('delete', channel.id, async (signal) => {
      await api.delete<{ id: number }>(
        `/api/channel/${channel.id}`,
        undefined,
        { signal }
      )
      toast.success(t('channels.deleted'))
      if (rows.value.length === 1 && page.value > 1) page.value -= 1
      else await load({ background: true })
    })
  }

  /**
   * Discover the upstream model list, either for a saved channel (by id) or
   * from in-form credentials before the channel exists. Used by the
   * "从上游获取" quick action in the channel form. Throws ApiError on failure.
   */
  async function fetchUpstreamModels(
    params: AdminChannelFetchModelsParams
  ): Promise<string[]> {
    const saved = 'channelId' in params && params.advancedCustom === undefined
    if (!allowed(saved ? 'operate' : 'sensitive_write'))
      throw new ApiError(t('channels.permissionDenied'), { status: 403 })
    const channelType =
      'channelId' in params
        ? rows.value.find((channel) => channel.id === params.channelId)?.type
        : params.type
    if (channelType !== undefined && isRetiredChannelType(channelType))
      throw new Error(t('channels.retiredHint'))
    const models =
      'channelId' in params && params.advancedCustom === undefined
        ? await api.get<string[]>(
            `/api/channel/fetch_models/${params.channelId}`
          )
        : await api.post<string[]>('/api/channel/fetch_models', {
            ...('channelId' in params
              ? { channel_id: params.channelId }
              : { type: params.type, key: params.key }),
            base_url: params.baseUrl,
            ...(params.advancedCustom === undefined
              ? {}
              : { advanced_custom: params.advancedCustom }),
          })
    return parseStringArray(models, '/api/channel/fetch_models')
  }

  async function runBulkAction(
    action: ChannelBulkAction,
    // Resolving false reports a partial outcome the request already showed.
    request: (signal: AbortSignal) => Promise<boolean | void>
  ): Promise<boolean> {
    if (
      !canMutate.value ||
      !allowed(action === 'delete' ? 'sensitive_write' : 'operate')
    )
      return false

    const controller = new AbortController()
    bulkController = controller
    bulkAction.value = action
    try {
      return (await request(controller.signal)) !== false
    } catch (error) {
      if (controller.signal.aborted) return false
      toast.error(channelErrorMessage(error))
      return false
    } finally {
      if (bulkController === controller) bulkController = null
      bulkAction.value = null
    }
  }

  function bulkStatusTargets(
    action: Extract<ChannelBulkAction, 'enable' | 'disable' | 'reset'>,
    channels: AdminChannel[]
  ): AdminChannel[] {
    if (action === 'enable') {
      return channels.filter((channel) => channel.status !== 1)
    }
    if (action === 'disable') {
      return channels.filter((channel) => channel.status !== 2)
    }
    return channels.filter((channel) => channel.status === 3)
  }

  function updateChannelsStatus(
    action: Extract<ChannelBulkAction, 'enable' | 'disable' | 'reset'>,
    channels: AdminChannel[]
  ): Promise<boolean> {
    const targets = bulkStatusTargets(
      action,
      channels.filter((channel) => !isRetiredChannelType(channel.type))
    )
    if (targets.length === 0) return Promise.resolve(false)
    const status = action === 'disable' ? 2 : 1

    return runBulkAction(action, async (signal) => {
      let response: ApiEnvelope<number>
      try {
        response = await api.postEnvelope<number>(
          '/api/channel/status/batch',
          {
            ids: targets.map((channel) => channel.id),
            status,
          },
          { signal }
        )
      } catch (error) {
        const partial = batchStatusFailure(error)
        if (!partial) throw error
        reportPartialStatus(partial, targets)
        await load({ background: true })
        return false
      }
      toast.success(
        t(
          action === 'enable'
            ? 'channels.bulkEnabled'
            : action === 'disable'
              ? 'channels.bulkDisabled'
              : 'channels.bulkResetDone',
          { count: response.data }
        )
      )
      if (hasPendingPublication(response))
        toast.warning(t('common.publicationPending'))
      await load({ background: true })
    })
  }

  function reportPartialStatus(
    partial: BatchStatusFailure,
    targets: AdminChannel[]
  ): void {
    const names = new Map(targets.map((channel) => [channel.id, channel.name]))
    const list = partial.failures
      .map(({ id, code }) =>
        t('channels.bulkStatusFailureItem', {
          id,
          name: names.get(id) ?? '',
          reason: CHANNEL_ERROR_KEYS[code] ? t(CHANNEL_ERROR_KEYS[code]) : code,
        })
      )
      .join(t('channels.bulkStatusFailureSeparator'))
    toast.warning(
      t('channels.bulkStatusPartial', {
        count: partial.changed,
        failed: partial.failures.length,
        list,
      })
    )
    if (hasPendingPublication(partial.envelope as ApiEnvelope<unknown>))
      toast.warning(t('common.publicationPending'))
  }

  function deleteSelectedChannels(channels: AdminChannel[]): Promise<boolean> {
    const ids = channels.map((channel) => channel.id)
    if (ids.length === 0) return Promise.resolve(false)

    return runBulkAction('delete', async (signal) => {
      const deleted = await api.post<number>(
        '/api/channel/batch',
        { ids },
        { signal }
      )
      toast.success(t('channels.bulkDeleted', { count: deleted }))
      const removesCurrentPage = rows.value.every((channel) =>
        ids.includes(channel.id)
      )
      if (removesCurrentPage && page.value > 1) page.value -= 1
      else await load({ background: true })
    })
  }

  async function runBalanceBatch(
    scope: ChannelBatchScope,
    channels: AdminChannel[],
    supplier?: string
  ): Promise<void> {
    if (!canMutate.value || !allowed('operate') || channels.length === 0) return

    const targets = channels
      .filter(supportsBalanceQuery)
      .map((channel) => ({ ...channel }))
    if (targets.length === 0) return
    const controller = new AbortController()
    batchController = controller
    batchProgress.value = {
      scope,
      supplier,
      total: targets.length,
      processed: 0,
      succeeded: 0,
      failed: 0,
    }

    let succeeded = 0
    let failed = 0

    try {
      for (let start = 0; start < targets.length; start += CHANNEL_BATCH_SIZE) {
        if (controller.signal.aborted) return
        const batch = targets.slice(start, start + CHANNEL_BATCH_SIZE)
        setBatchRowsBusy(batch, true)

        const results = await Promise.allSettled(
          batch.map((channel) =>
            channelOperation(
              `/api/channel/update_balance/${channel.id}`,
              undefined,
              controller.signal
            )
          )
        )
        if (controller.signal.aborted) return

        results.forEach((result) => {
          if (result.status === 'fulfilled') {
            succeeded++
          } else {
            failed++
          }
        })
        setBatchRowsBusy(batch, false)
        batchProgress.value = {
          scope,
          supplier,
          total: targets.length,
          processed: succeeded + failed,
          succeeded,
          failed,
        }
      }

      const actionLabel = t('channels.batchBalanceAction')
      if (failed === 0) {
        toast.success(
          t('channels.batchCompleted', {
            action: actionLabel,
            count: succeeded,
          })
        )
      } else if (succeeded === 0) {
        toast.error(
          t('channels.batchFailed', {
            action: actionLabel,
            count: failed,
          })
        )
      } else {
        toast.warning(
          t('channels.batchPartial', {
            action: actionLabel,
            succeeded,
            total: targets.length,
            failed,
          })
        )
      }
      await load({ background: true })
    } finally {
      setBatchRowsBusy(targets, false)
      if (batchController === controller) batchController = null
      batchProgress.value = null
    }
  }

  function runSupplierBalance(
    supplier: string,
    targets: AdminChannel[],
    _groupSlug?: string
  ) {
    return runBalanceBatch('supplier', targets, supplier)
  }
  function deleteSupplierChannels(
    _supplier: string,
    targets: AdminChannel[],
    _groupSlug?: string
  ) {
    return deleteSelectedChannels(targets)
  }
  function runVisibleBalance(): Promise<void> {
    return runBalanceBatch('page', rows.value)
  }

  watch(canRead, (readable) => {
    listRequest.cancel()
    batchController?.abort()
    crudController?.abort()
    bulkController?.abort()
    rows.value = []
    total.value = 0
    typeCounts.value = {}
    loading.value = false
    refreshing.value = false
    if (readable) void load()
  })
  onMounted(() => {
    if (canRead.value) void load()
    else loading.value = false
  })
  onBeforeUnmount(() => {
    batchController?.abort()
    crudController?.abort()
    bulkController?.abort()
    window.clearTimeout(searchTimer)
  })

  return {
    rows,
    total,
    typeCounts,
    page,
    pageSize,
    keyword,
    modelKeyword,
    type,
    lab,
    labs,
    status,
    hasFilters,
    resetFilters,
    sortBy,
    sortOrder,
    loading,
    refreshing,
    initialError,
    batchProgress,
    balanceResponse,
    isBatchBusy,
    isCrudBusy,
    isBulkBusy,
    canMutate,
    canRunBatch,
    load,
    isBusy,
    isRowBusy,
    isCrudActionBusy,
    isBulkActionBusy,
    updateNumber,
    toggleStatus,
    refreshBalance,
    testChannelModel,
    removeChannelModels,
    createChannel,
    updateChannelDetails,
    deleteChannel,
    fetchUpstreamModels,
    updateChannelsStatus,
    deleteSelectedChannels,
    runVisibleBalance,
    runSupplierBalance,
    deleteSupplierChannels,
  }
}
