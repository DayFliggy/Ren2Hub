import type { TopupOrder } from '@/api/topup'
import { serializeSpreadsheet } from '@/utils/spreadsheetExport'

const fields = [
  'id',
  'user_id',
  'trade_no',
  'amount',
  'money',
  'payment_provider',
  'payment_method',
  'status',
  'create_time',
  'complete_time',
] as const
export function serializeOrders(
  rows: TopupOrder[],
  format: string
): [string, string, string] {
  // Export only documented order fields; never include future credential fields.
  const orders = rows.map((row) =>
    Object.fromEntries(fields.map((key) => [key, row[key]]))
  )
  if (format === 'json')
    return [
      JSON.stringify(orders, null, 2),
      'application/json;charset=utf-8',
      'json',
    ]
  return serializeSpreadsheet(
    fields,
    orders.map((row) => fields.map((key) => row[key])),
    format === 'excel' ? 'excel' : 'csv'
  )
}
