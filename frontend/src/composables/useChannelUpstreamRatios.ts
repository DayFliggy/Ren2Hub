import { ref, watch, type Ref } from 'vue'

import {
  channelUpstreamRatioApi,
  type ChannelUpstreamRatio,
} from '@/api/channelUpstreamRatio'
import { ApiError } from '@/api/types'
import { useLatestRequest } from '@/composables/useLatestRequest'
import type { AdminChannel } from '@/types/console'

/**
 * Loads the display-only upstream ratios for the visible channel page. The
 * list loads independently so a ratio failure never blocks the channel table.
 */
export function useChannelUpstreamRatios(
  rows: Ref<AdminChannel[]>,
  canRead: Ref<boolean>
) {
  const ratios = ref<Record<number, ChannelUpstreamRatio>>({})
  const error = ref('')
  const request = useLatestRequest()
  // useLatestRequest 在请求完成时判定是否过时，到这里继续执行前可能已开始下一次加载；
  // 本地序号在使用结果的位置再确认一次，被取代的加载不写入。
  let loadSequence = 0

  async function reload() {
    const sequence = ++loadSequence
    const ids = rows.value.map((channel) => channel.id)
    if (!canRead.value || !ids.length) {
      request.cancel()
      ratios.value = {}
      error.value = ''
      return
    }
    // 请求前的快照：只有请求期间被弹窗保存替换过的条目才需要保护。
    const before = ratios.value
    const result = await request.run((signal) =>
      channelUpstreamRatioApi.list(ids, signal)
    )
    if (result.stale || sequence !== loadSequence) return
    if (!result.ok) {
      // 读取失败时无法确认旧值仍对应当前渠道配置，保留数值但标为过期；
      // 请求期间由弹窗保存的条目是新结果，不标过期。
      ratios.value = Object.fromEntries(
        Object.values(ratios.value).map((view) => [
          view.channel_id,
          view === before[view.channel_id] ? { ...view, stale: true } : view,
        ])
      )
      error.value =
        result.error instanceof ApiError
          ? result.error.message
          : String(result.error)
      return
    }
    error.value = ''
    // 弹窗保存可能在列表请求途中完成，此时保留版本更新的保存结果。其余条目一律以响应为准，
    // 同 ID 重建后版本从 1 重新开始也能正确替换。
    const current = ratios.value
    ratios.value = Object.fromEntries(
      result.value.map((item) => {
        const saved = current[item.channel_id]
        const savedDuringRequest =
          saved !== undefined && saved !== before[item.channel_id]
        return [
          item.channel_id,
          savedDuringRequest && saved.version > item.version ? saved : item,
        ]
      })
    )
  }

  function update(view: ChannelUpstreamRatio) {
    ratios.value = { ...ratios.value, [view.channel_id]: view }
  }

  // 每次列表加载都会替换 rows 数组，编辑后刷新即使 ID 不变也会重新读取倍率。
  watch(rows, () => void reload(), { immediate: true })

  return { ratios, error, reload, update }
}
