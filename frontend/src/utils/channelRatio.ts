import { compareDecimal, isDecimal } from './decimal'

/** Shared native channel and private-route limit: (0, 1000], at most 18 decimals. */
export function isValidChannelRatio(value: string): boolean {
  return (
    isDecimal(value) &&
    (value.split('.')[1]?.length ?? 0) <= 18 &&
    compareDecimal(value, '0') > 0 &&
    compareDecimal(value, '1000') <= 0
  )
}
