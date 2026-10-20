import {
  invalidResponse,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import type { AdminChannel } from '@/types/console'

export function parseChannelStatusDetails(channel: AdminChannel): AdminChannel {
  const endpoint = '/api/next/admin/channels'
  const reason =
    channel.status_reason === undefined
      ? ''
      : requiredString(channel.status_reason, endpoint)
  const time =
    channel.status_time === undefined
      ? 0
      : requiredStrictInteger(channel.status_time, endpoint)
  if (time < 0 || time > 253402300799) invalidResponse(endpoint)
  return { ...channel, status_reason: reason, status_time: time }
}
